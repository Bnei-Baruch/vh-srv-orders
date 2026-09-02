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

	// Both start checks run before the poll, so neither is charged for the time it
	// spends: a tolerance read after the poll would fail on the poll's own
	// duration once that budget approaches it.
	//
	// The start was passed as a parameter from the Go clock, so comparing it to the
	// Go clock is exact and no skew enters. The future-dating check has to be here
	// rather than left to the poll, which cannot tell a future-dated start from a
	// database that is behind and would retry both.
	assert.False(t, grant.StartDate.After(time.Now()),
		"the default start is in the future: %s", grant.StartDate)
	// Catches start and end written to the wrong columns. It cannot show the Go
	// default was used: start is passed as a parameter, so this compares Go's
	// clock against Go's own value.
	assert.WithinDuration(t, time.Now(), grant.StartDate, time.Minute)

	// GetActiveHHGrant filters start_date <= NOW() on the database clock, against
	// that Go-clock start, so a database behind the host has not reached it yet.
	// Polled, with the same order of slack the rest of this file allows for skew;
	// the budget is skew tolerance only, since the assertion above already pins
	// the part that must hold immediately. Only the no-rows result is retried: a
	// query error would otherwise spend the whole budget and be reported as skew.
	//
	// Not require.Eventually — its failure-message arguments are evaluated before
	// the first poll.
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
	// Coarse on purpose: an exact expectation here means either reimplementing
	// Postgres month arithmetic in Go — the bug this file started with — or
	// copying the production expression. Pinned exactly in the clamping test.
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
	// Start a minute back, since this test does not care where the start comes
	// from: cheaper than the default-start test's poll, and tolerant of more skew
	// than that poll's budget.
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved: true, Type: common.HHGrantTypeGimlaj, DiscountPct: 50, Months: 3,
		StartDate: null.TimeFrom(time.Now().Add(-time.Minute)),
	})
	require.NoError(t, err)

	// GetActiveHHGrant orders by id DESC LIMIT 1, so it returns the new grant
	// whether or not the old one was ended — this has to be asked directly. Skew
	// still enters: the row is only ended if ConcludeHHRequest's UPDATE matched
	// start_date <= NOW() against insertHHGrant's Go-clock start, an hour back.
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
// the month can hold. The start is pinned and historical so the assertion never
// depends on the run date, which means reading through GetAllHHRequests —
// a grant that has already ended fails GetActiveHHGrant's end_date > NOW().
//
// DEPENDS ON #19: the historical start only works because ConcludeHHRequest
// accepts one. If #19 is fixed by rejecting past starts, the conclude call below
// fails and this clamping assertion never runs — repin the start on a 31st the
// fix allows, keeping it absolute so the test stays date-independent, rather
// than loosening the assertion. A fix that instead leaves the live grant alone
// does not touch this test.
func TestConcludeHHRequest_Approve_ClampsEndDateToAShorterMonth(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-clamp"))
	require.NoError(t, err)

	// Three, where the request asked for six: the approved term is the admin's,
	// and an implementation reading request.Months would pass with both the same.
	// 31 August plus three months is 31 November, which does not exist.
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
	// Whatever its dates — this join has no NOW() filter. That a past start_date
	// is accepted at all is issue #19, not intended behaviour.
	require.NotNil(t, joined[0].Grant, "the join returns the grant whatever its dates")

	// Exact instants because pkg/testutil pins the session to UTC — reproducible,
	// not right: the expression adds months in the session timezone. A whole-hour
	// offset here means the connection string, not the grant code.
	assert.Equal(t, start, joined[0].Grant.StartDate.UTC(), "start is stored as given")
	assert.Equal(t, time.Date(2020, 11, 30, 12, 0, 0, 0, time.UTC), joined[0].Grant.EndDate.UTC())
}

// withSessionTimezone repoints the timezone carried by the `options` startup
// parameter of a pgtestdb URL. Parsed, not string-replaced, and only the
// timezone setting is touched: the pin's encoding is pkg/testutil's business,
// and any other -c setting it grows later has to survive.
//
// Splits on whitespace, so it holds for settings with whitespace-free values —
// all the pin has ever carried. A value with an escaped space would need the
// libpq quoting rules, which is not worth carrying here until something needs it.
func withSessionTimezone(t *testing.T, dbURL, timezone string) string {
	t.Helper()
	u, err := url.Parse(dbURL)
	require.NoError(t, err)
	query := u.Query()

	// Matched case-insensitively: Postgres parameter names are, so the pin is free
	// to say TimeZone= or timezone= and this must find either.
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
	// Encode renders a space as "+", which libpq sends literally: psql then fails
	// with `unrecognized configuration parameter "+timezone"`. A literal plus is
	// already "%2B" by here, so only spaces are affected.
	u.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	return u.String()
}

// CHARACTERIZATION: pins the end-date behaviour of issue #20, which every other
// test here is blind to under pkg/testutil's UTC pin — hence its own pool. Going
// red is the intended signal that #20 is fixed; delete this test then.
//
// The hour comes from DST, not from the offset: Asia/Jerusalem is +03 (IDT) on
// 31 August 2020, so 12:00Z is 15:00 local, and by the clamped 30 November the
// zone is back on +02 (IST), making 15:00 local 13:00Z. A fixed-offset zone such
// as Etc/GMT-2 gives 12:00Z and would fail here with no bug present: the offset
// cancels, the shift between the two dates does not.
func TestConcludeHHRequest_EndDateDependsOnSessionTimezone(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	jerusalemURL := withSessionTimezone(t, dbURL, "Asia/Jerusalem")

	jerusalem, err := NewOrdersDBUrl(ctx, jerusalemURL, new(events.NoopEmitter))
	// The server rejects the startup parameter outright if it does not know the
	// zone, so this fails before the guard below with pgx's own wording.
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
