package repo

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

func hhRequestReq(keycloakID string) HHRequestReq {
	return HHRequestReq{
		KeycloakID:   keycloakID,
		Type:         common.HHGrantTypeGimlaj,
		RequestedPct: 80,
		Months:       6,
		Note:         null.StringFrom("my situation"),
	}
}

func TestCreateHHRequest_CreatesPendingRequest(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-create"))
	require.NoError(t, err)
	assert.NotZero(t, r.ID)
	assert.Equal(t, common.HHRequestStatusRequested, r.Status)
	assert.Equal(t, common.HHGrantTypeGimlaj, r.Type)
	assert.Equal(t, 80, r.RequestedPct)
	assert.Equal(t, 6, r.Months)
}

func TestCreateHHRequest_ReplacesPendingRequest(t *testing.T) {
	db, ctx := newTestDB(t)

	first, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-replace"))
	require.NoError(t, err)
	second, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-replace"))
	require.NoError(t, err)

	all, err := db.GetAllHHRequests(ctx, "", "kc-req-replace")
	require.NoError(t, err)
	require.Len(t, all, 1, "previous pending request is deleted")
	assert.Equal(t, second.ID, all[0].ID)
	assert.NotEqual(t, first.ID, second.ID)
}

func TestConcludeHHRequest_Approve_CreatesGrant(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-approve"))
	require.NoError(t, err)

	// No pinned start, covering the time.Now() default.
	concluded, err := db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:    true,
		Type:        common.HHGrantTypeHayal, // admin overrides the requested type
		DiscountPct: 75,
		Months:      6,
		Note:        null.StringFrom("approved grant"),
	})
	require.NoError(t, err)
	assert.Equal(t, common.HHRequestStatusApproved, concluded.Status)

	joined, err := db.GetAllHHRequests(ctx, "", "kc-req-approve")
	require.NoError(t, err)
	require.Len(t, joined, 1)
	grant := joined[0].Grant
	require.NotNil(t, grant, "approval creates a grant")

	// Before the poll, and require: the poll cannot tell a future-dated start
	// from a database that is behind, so it would report either as clock skew.
	require.False(t, grant.StartDate.After(time.Now()),
		"the default start is in the future: %s", grant.StartDate)
	require.WithinDuration(t, time.Now(), grant.StartDate, time.Minute)

	// GetActiveHHGrant filters start_date <= NOW() on the database clock, so a
	// database behind the host has not reached this Go-clock start yet. Polled
	// for that skew, retrying no-rows only. Not require.Eventually, which
	// evaluates its message arguments before the first poll.
	var active *HHGrant
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		active, err = db.GetActiveHHGrant(ctx, "kc-req-approve")
		require.NoError(t, err)
		if active != nil {
			break
		}
		if time.Now().After(deadline) {
			require.FailNowf(t, "a grant starting now never became active",
				"no error, so compare the database clock against %s", time.Now().UTC())
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.Equal(t, grant.ID, active.ID, "both read paths return the same grant")

	assert.Equal(t, r.ID, grant.RequestID, "grant is linked to its request")
	assert.Equal(t, 75, grant.DiscountPct)
	assert.Equal(t, common.HHGrantTypeHayal, grant.Type)
	// Coarse: an exact expectation means reimplementing Postgres month
	// arithmetic in Go, which is the bug this file started with. Pinned exactly
	// in the clamping test.
	assert.True(t, grant.EndDate.After(grant.StartDate.AddDate(0, 5, 0)),
		"end %s is less than five months after start %s", grant.EndDate, grant.StartDate)
	assert.True(t, grant.EndDate.Before(grant.StartDate.AddDate(0, 7, 0)),
		"end %s is more than seven months after start %s", grant.EndDate, grant.StartDate)
}

func TestConcludeHHRequest_Approve_ReplacesActiveGrant(t *testing.T) {
	db, ctx := newTestDB(t)

	oldID := insertHHGrant(t, db, ctx, "kc-req-regrant", -time.Hour, 24*time.Hour)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-regrant"))
	require.NoError(t, err)
	// A minute back, so no poll is needed: this test does not care where the
	// start comes from.
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved: true, Type: common.HHGrantTypeGimlaj, DiscountPct: 50, Months: 3,
		StartDate: null.TimeFrom(time.Now().Add(-time.Minute)),
	})
	require.NoError(t, err)

	// GetActiveHHGrant orders by id DESC LIMIT 1, so it returns the new grant
	// whether or not the old one was ended. Asked directly.
	var oldEnded bool
	require.NoError(t, db.QueryRow(ctx,
		`SELECT end_date < NOW() FROM hh_grants WHERE id = $1`, oldID).Scan(&oldEnded))
	assert.True(t, oldEnded, "the previous grant should have been ended")

	active, err := db.GetActiveHHGrant(ctx, "kc-req-regrant")
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.NotEqual(t, oldID, active.ID, "the new grant is the active one")
	assert.Equal(t, 50, active.DiscountPct)
}

func TestConcludeHHRequest_Deny_NoGrant(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-deny"))
	require.NoError(t, err)

	concluded, err := db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:      false,
		RejectionNote: null.StringFrom("not eligible"),
	})
	require.NoError(t, err)
	assert.Equal(t, common.HHRequestStatusDenied, concluded.Status)
	assert.Equal(t, "not eligible", concluded.RejectionNote.String)

	grant, err := db.GetActiveHHGrant(ctx, "kc-req-deny")
	require.NoError(t, err)
	assert.Nil(t, grant)
}

func TestConcludeHHRequest_AlreadyConcluded_ReturnsErrNoRowsAffected(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-twice"))
	require.NoError(t, err)
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{Approved: false})
	require.NoError(t, err)

	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{Approved: false})
	require.ErrorIs(t, err, common.ErrNoRowsAffected)
}

func TestGetAllHHRequests_FiltersByStatusAndKcid(t *testing.T) {
	db, ctx := newTestDB(t)

	r1, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-f1"))
	require.NoError(t, err)
	_, err = db.CreateHHRequest(ctx, hhRequestReq("kc-req-f2"))
	require.NoError(t, err)
	_, err = db.ConcludeHHRequest(ctx, r1.ID, HHRequestConclusion{Approved: false})
	require.NoError(t, err)

	pending, err := db.GetAllHHRequests(ctx, common.HHRequestStatusRequested, "")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "kc-req-f2", pending[0].KeycloakID)

	byKcid, err := db.GetAllHHRequests(ctx, "", "kc-req-f1")
	require.NoError(t, err)
	require.Len(t, byKcid, 1)
	assert.Equal(t, common.HHRequestStatusDenied, byKcid[0].Status)
}

// Three months from 31 August is 30 November: Postgres clamps to the last day
// the month holds. The start is historical so the assertion never depends on the
// run date, which is why this reads through GetAllHHRequests — an ended grant
// fails GetActiveHHGrant's end_date > NOW().
//
// Depends on #19: if past starts are rejected, repin on a 31st the fix allows
// rather than loosening the assertion.
func TestConcludeHHRequest_Approve_ClampsEndDateToAShorterMonth(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-clamp"))
	require.NoError(t, err)

	// Three where the request asked for six, so an implementation reading
	// request.Months fails here.
	start := time.Date(2020, 8, 31, 12, 0, 0, 0, time.UTC)
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:    true,
		Type:        common.HHGrantTypeGimlaj,
		DiscountPct: 50,
		Months:      3,
		StartDate:   null.TimeFrom(start),
	})
	require.NoError(t, err)

	joined, err := db.GetAllHHRequests(ctx, "", "kc-req-clamp")
	require.NoError(t, err)
	require.Len(t, joined, 1)
	require.NotNil(t, joined[0].Grant, "the join returns the grant whatever its dates")

	// Exact instants only because pkg/testutil pins the session to UTC; the
	// expression adds months in the session timezone. A whole-hour offset here
	// means the connection string, not the grant code.
	assert.Equal(t, start, joined[0].Grant.StartDate.UTC(), "start is stored as given")
	assert.Equal(t, time.Date(2020, 11, 30, 12, 0, 0, 0, time.UTC), joined[0].Grant.EndDate.UTC())
}

// withSessionTimezone repoints the timezone in the `options` startup parameter
// of a pgtestdb URL, leaving any other -c setting alone.
func withSessionTimezone(t *testing.T, dbURL, timezone string) string {
	t.Helper()
	u, err := url.Parse(dbURL)
	require.NoError(t, err)
	// Not u.Query(), which drops pairs it cannot parse and hides the error —
	// RawQuery is rebuilt from this map, so sslmode would vanish with it.
	query, err := url.ParseQuery(u.RawQuery)
	require.NoError(t, err)

	// Case-insensitive, as Postgres parameter names are.
	settings := strings.Fields(query.Get("options"))
	found := 0
	for i, setting := range settings {
		if strings.HasPrefix(strings.ToLower(setting), "timezone=") {
			settings[i] = "timezone=" + timezone
			found++
		}
	}
	require.Equal(t, 1, found, "test URL should carry exactly one pinned timezone")

	query.Set("options", strings.Join(settings, " "))
	// Encode renders a space as "+", which libpq sends literally. A real plus is
	// already %2B by here, so only spaces are affected.
	u.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	return u.String()
}

// CHARACTERIZATION of issue #20, on its own pool because the UTC pin hides it.
// Going red means #20 is fixed; delete the test then.
//
// The hour comes from DST, not the offset: 12:00Z is 15:00 IDT (+03) on 31
// August, and 15:00 IST (+02) on the clamped 30 November is 13:00Z. A
// fixed-offset zone cancels out and would fail here with no bug present.
func TestConcludeHHRequest_EndDateDependsOnSessionTimezone(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	jerusalemURL := withSessionTimezone(t, dbURL, "Asia/Jerusalem")

	jerusalem, err := NewOrdersDBUrl(ctx, jerusalemURL, new(events.NoopEmitter))
	require.NoError(t, err, "connecting with TimeZone=Asia/Jerusalem: does the "+
		"server have tzdata for it?")
	t.Cleanup(jerusalem.Close)

	var tz string
	require.NoError(t, jerusalem.QueryRow(ctx, "SHOW TimeZone").Scan(&tz))
	require.Equal(t, "Asia/Jerusalem", tz, "second pool did not take the timezone")

	r, err := jerusalem.CreateHHRequest(ctx, hhRequestReq("kc-req-tz"))
	require.NoError(t, err)

	start := time.Date(2020, 8, 31, 12, 0, 0, 0, time.UTC)
	_, err = jerusalem.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:    true,
		Type:        common.HHGrantTypeGimlaj,
		DiscountPct: 50,
		Months:      3,
		StartDate:   null.TimeFrom(start),
	})
	require.NoError(t, err)

	joined, err := jerusalem.GetAllHHRequests(ctx, "", "kc-req-tz")
	require.NoError(t, err)
	require.Len(t, joined, 1)
	require.NotNil(t, joined[0].Grant)

	assert.Equal(t, time.Date(2020, 11, 30, 13, 0, 0, 0, time.UTC),
		joined[0].Grant.EndDate.UTC(),
		"end date no longer shifts with the session timezone — if you fixed issue "+
			"#20, delete this test")
}
