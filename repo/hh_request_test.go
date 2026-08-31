package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// addMonthsClamped adds months the way Postgres does, which is how the grant's
// end_date is actually computed:
//
//	end_date = start_date::timestamptz + make_interval(months => n)
//
// Two differences from time.AddDate, both of which made this test fail on
// 2026-08-31 while passing on most days:
//
// Postgres clamps a day the target month is too short to hold to that month's
// last day, where AddDate rolls it into the next month — Feb 28 against Mar 3,
// three days apart.
//
// And the arithmetic happens in the session's timezone, UTC here, not in local
// wall-clock time. Adding six calendar months to 16:25 IDT (+03) and to 13:25
// UTC lands on instants an hour apart, because the target date is in +02. That
// half is invisible on a UTC CI runner and shows up locally.
func addMonthsClamped(t time.Time, months int) time.Time {
	t = t.UTC()
	year, month, day := t.Date()

	firstOfTarget := time.Date(year, month+time.Month(months), 1,
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	if lastDay := firstOfTarget.AddDate(0, 1, -1).Day(); day > lastDay {
		day = lastDay
	}

	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), day,
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

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
	assert.WithinDuration(t, addMonthsClamped(time.Now(), 6), grant.EndDate, time.Minute)

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

// The cases that made the original assertion fail, plus a leap year. Pinned
// because the failure only appears on a few days a year, which is the worst
// property a test can have: it looks fine in review and breaks unrelated work
// months later.
func TestAddMonthsClamped(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		months int
		want   string
	}{
		{"31 Aug to a 28-day February", "2026-08-31", 6, "2027-02-28"},
		{"31 Aug to a 29-day February", "2027-08-31", 6, "2028-02-29"},
		{"31 Mar to a 30-day September", "2026-03-31", 6, "2026-09-30"},
		{"31 Jan to a 31-day July", "2026-01-31", 6, "2026-07-31"},
		{"a day every month can hold", "2026-08-15", 6, "2027-02-15"},
		{"crossing the year boundary", "2026-11-30", 3, "2027-02-28"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, err := time.Parse(time.RFC3339, c.from+"T12:00:00Z")
			require.NoError(t, err)

			got := addMonthsClamped(from, c.months)
			if want := c.want + "T12:00:00Z"; got.Format(time.RFC3339) != want {
				t.Errorf("addMonthsClamped(%s, %d) = %s, want %s",
					c.from, c.months, got.Format(time.RFC3339), want)
			}
		})
	}
}
