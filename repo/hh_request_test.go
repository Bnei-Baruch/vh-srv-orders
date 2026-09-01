package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
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

	// No pinned start, covering the time.Now() default. Read through
	// GetAllHHRequests: GetActiveHHGrant filters start_date <= NOW() on the
	// database clock, which a Go-clock start flakes against.
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

	assert.Equal(t, r.ID, grant.RequestID, "grant is linked to its request")
	assert.Equal(t, 75, grant.DiscountPct)
	assert.Equal(t, common.HHGrantTypeHayal, grant.Type)
	// Catches start and end written to the wrong columns. It cannot show the Go
	// default was used: start is passed as a parameter, so this compares Go's
	// clock against Go's own value.
	assert.WithinDuration(t, time.Now(), grant.StartDate, time.Minute)
	// Coarse on purpose: pinning it exactly would mean reimplementing Postgres
	// month arithmetic in Go, which is the bug this file started with.
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
	// Start a minute back: GetActiveHHGrant compares start_date against the
	// database clock, and a start of "now" from the Go clock flakes on skew.
	_, err = db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved: true, Type: common.HHGrantTypeGimlaj, DiscountPct: 50, Months: 3,
		StartDate: null.TimeFrom(time.Now().Add(-time.Minute)),
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

// Three months from 31 August is 30 November: Postgres clamps to the last day
// the month can hold. The start is pinned and historical so the assertion never
// depends on the run date, which means reading through GetAllHHRequests —
// a grant that has already ended fails GetActiveHHGrant's end_date > NOW().
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
