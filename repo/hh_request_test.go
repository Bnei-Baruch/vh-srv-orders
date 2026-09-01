package repo

import (
	"context"
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

	// No pinned start: this test is about approval producing an *active* grant,
	// and GetActiveHHGrant filters on end_date > NOW() AND start_date <= NOW().
	// A fixed historical start would make it return nothing. What the end date is
	// computed to is asserted separately, where it can be pinned safely.
	concluded, err := db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:    true,
		Type:        common.HHGrantTypeHayal, // admin overrides the requested type
		DiscountPct: 75,
		Months:      6,
		Note:        null.StringFrom("approved grant"),
	})
	require.NoError(t, err)
	assert.Equal(t, common.HHRequestStatusApproved, concluded.Status)

	grant, err := db.GetActiveHHGrant(ctx, "kc-req-approve")
	require.NoError(t, err)
	require.NotNil(t, grant, "approval should create an active grant")
	assert.Equal(t, r.ID, grant.RequestID, "grant is linked to its request")
	assert.Equal(t, 75, grant.DiscountPct)
	assert.Equal(t, common.HHGrantTypeHayal, grant.Type)
	// The only coverage of the default start at repo/hh_request.go: the clamping
	// test always passes an explicit one, and GetActiveHHGrant's start_date <=
	// NOW() would accept a default a month in the past.
	assert.WithinDuration(t, time.Now(), grant.StartDate, time.Minute)

	joined, err := db.GetAllHHRequests(ctx, "", "kc-req-approve")
	require.NoError(t, err)
	require.Len(t, joined, 1)
	require.NotNil(t, joined[0].Grant, "joined fetch embeds the grant")
	assert.Equal(t, grant.ID, joined[0].Grant.ID)
}

func TestConcludeHHRequest_Approve_ReplacesActiveGrant(t *testing.T) {
	db, ctx := newTestDB(t)

	oldID := insertHHGrant(t, db, ctx, "kc-req-regrant", -time.Hour, 24*time.Hour)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-regrant"))
	require.NoError(t, err)
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved: true, Type: common.HHGrantTypeGimlaj, DiscountPct: 50, Months: 3,
	})
	require.NoError(t, err)

	active, err := db.GetActiveHHGrant(ctx, "kc-req-regrant")
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.NotEqual(t, oldID, active.ID, "previous grant ended, new one active")
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

// The end date is start + months as Postgres computes it, clamped to the last
// day a short month can hold: six months from 31 August is 28 February, not
// 3 March where day arithmetic lands. A grant approved on the 31st must not
// outlast one approved on the 30th.
//
// The start is pinned, and deliberately historical, so the assertion does not
// depend on when the suite runs. That means reading the grant through
// GetAllHHRequests, which joins hh_grants unconditionally — GetActiveHHGrant
// filters on end_date > NOW() and would return nothing for a grant that has
// already expired.
func TestConcludeHHRequest_Approve_ClampsEndDateToAShorterMonth(t *testing.T) {
	db, ctx := newTestDB(t)

	r, err := db.CreateHHRequest(ctx, hhRequestReq("kc-req-clamp"))
	require.NoError(t, err)

	start := time.Date(2020, 8, 31, 12, 0, 0, 0, time.UTC)
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:    true,
		Type:        common.HHGrantTypeGimlaj,
		DiscountPct: 50,
		Months:      6,
		StartDate:   null.TimeFrom(start),
	})
	require.NoError(t, err)

	joined, err := db.GetAllHHRequests(ctx, "", "kc-req-clamp")
	require.NoError(t, err)
	require.Len(t, joined, 1)
	require.NotNil(t, joined[0].Grant, "approval creates a grant regardless of its dates")

	assert.Equal(t, start, joined[0].Grant.StartDate.UTC(), "start is stored as given")
	assert.Equal(t, time.Date(2021, 2, 28, 12, 0, 0, 0, time.UTC), joined[0].Grant.EndDate.UTC())
}

// The end date must not depend on the server's timezone, asserted through
// ConcludeHHRequest rather than against the SQL expression on its own.
//
// This is the only test that can catch the regression. pkg/testutil pins every
// session to UTC, where the fixed and unfixed expressions agree, so the clamping
// test above passes either way — and an assertion against the expression string
// passes even if the INSERT stops using it. So: a second pool onto the same
// instance database with a non-UTC session, and the production path run over it.
func TestConcludeHHRequest_EndDateIgnoresServerTimezone(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	jerusalemURL := strings.Replace(dbURL, "timezone%3DUTC", "timezone%3DAsia%2FJerusalem", 1)
	require.NotEqual(t, dbURL, jerusalemURL, "test URL should carry the pinned timezone")

	jerusalem, err := NewOrdersDBUrl(ctx, jerusalemURL, new(events.NoopEmitter))
	require.NoError(t, err)
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
		Months:      6,
		StartDate:   null.TimeFrom(start),
	})
	require.NoError(t, err)

	joined, err := jerusalem.GetAllHHRequests(ctx, "", "kc-req-tz")
	require.NoError(t, err)
	require.Len(t, joined, 1)
	require.NotNil(t, joined[0].Grant)

	// Same instant a UTC server produces. Without the explicit conversion this is
	// 13:00 UTC, an hour of grant length decided by server configuration.
	assert.Equal(t, time.Date(2021, 2, 28, 12, 0, 0, 0, time.UTC), joined[0].Grant.EndDate.UTC())
}
