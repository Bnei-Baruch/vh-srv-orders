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
// And the arithmetic happens in the session's timezone, not in local wall-clock
// time. Adding six calendar months to 16:25 IDT (+03) and to 13:25 UTC lands on
// instants an hour apart, because the target date is in +02. That half is
// invisible on a UTC CI runner and shows up locally.
//
// The session timezone is UTC because pkg/testutil pins it on the connection
// URL, which is what makes the from.UTC() here correct rather than lucky.
func addMonthsClamped(from time.Time, months int) time.Time {
	from = from.UTC()
	year, month, day := from.Date()

	firstOfTarget := time.Date(year, month+time.Month(months), 1,
		from.Hour(), from.Minute(), from.Second(), from.Nanosecond(), from.Location())
	if lastDay := firstOfTarget.AddDate(0, 1, -1).Day(); day > lastDay {
		day = lastDay
	}

	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), day,
		from.Hour(), from.Minute(), from.Second(), from.Nanosecond(), from.Location())
}

// clampedWindow brackets where an end date computed from an unobservable
// time.Now() between before and after can land.
//
// It orders the pair rather than trusting addMonthsClamped to be monotonic,
// because clamping makes it not so: 30 August and 31 August both clamp to
// 28 February, but the time of day carries over, so the later input produces the
// *earlier* instant. Straddle a UTC midnight into a day that clamps and the
// naive window inverts —
//
//	before 2026-08-30 23:59:59.9Z -> 2027-02-28 23:59:59.9
//	after  2026-08-31 00:00:00.1Z -> 2027-02-28 00:00:00.1
//
// which is a range nothing can satisfy. Microseconds wide, a few nights a year,
// and exactly the class of latent date bug this file exists to stop.
func clampedWindow(before, after time.Time, months int) (earliest, latest time.Time) {
	earliest, latest = addMonthsClamped(before, months), addMonthsClamped(after, months)
	if latest.Before(earliest) {
		earliest, latest = latest, earliest
	}
	return earliest, latest
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

	// ConcludeHHRequest stamps its own time.Now(), so bracket the call rather
	// than sampling the clock again afterwards: across a UTC month boundary the
	// two samples clamp to different days and the expectation moves by 24h.
	before := time.Now()
	concluded, err := db.ConcludeHHRequest(ctx, r.ID, HHRequestConclusion{
		Approved:    true,
		Type:        common.HHGrantTypeHayal, // admin overrides the requested type
		DiscountPct: 75,
		Months:      6,
		Note:        null.StringFrom("approved grant"),
	})
	require.NoError(t, err)
	after := time.Now()
	assert.Equal(t, common.HHRequestStatusApproved, concluded.Status)

	grant, err := db.GetActiveHHGrant(ctx, "kc-req-approve")
	require.NoError(t, err)
	require.NotNil(t, grant, "approval should create an active grant")
	assert.Equal(t, r.ID, grant.RequestID, "grant is linked to its request")
	assert.Equal(t, 75, grant.DiscountPct)
	assert.Equal(t, common.HHGrantTypeHayal, grant.Type)
	earliest, latest := clampedWindow(before, after, 6)
	assert.False(t, grant.EndDate.Before(earliest.Add(-time.Minute)),
		"end date %s is before six months from %s", grant.EndDate, before)
	assert.False(t, grant.EndDate.After(latest.Add(time.Minute)),
		"end date %s is after six months from %s", grant.EndDate, after)

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

// The clamping rule, on the date that exposed it. A grant approved on 31 August
// must not outlast one approved on 30 August.
func TestAddMonthsClamped_ClampsToAShorterMonth(t *testing.T) {
	from := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2027, 2, 28, 12, 0, 0, 0, time.UTC), addMonthsClamped(from, 6))

	from = time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), addMonthsClamped(from, 6))
}

func TestAddMonthsClamped_LeapYearFebruaryHolds29(t *testing.T) {
	from := time.Date(2027, 8, 31, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC), addMonthsClamped(from, 6))
}

func TestAddMonthsClamped_KeepsTheDayWhenTheMonthIsLongEnough(t *testing.T) {
	from := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2027, 2, 15, 12, 0, 0, 0, time.UTC), addMonthsClamped(from, 6))

	from = time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2027, 2, 28, 12, 0, 0, 0, time.UTC), addMonthsClamped(from, 3))
}

// The half the first version of this guard missed. Every other case here is
// already UTC, so they pass whether or not the helper converts — the bug lived
// exactly in that gap.
//
// 01:30 on 31 August in Jerusalem is 22:30 on the 30th in UTC, so the month
// arithmetic starts from a different day and lands on a different instant than
// local wall-clock arithmetic would.
func TestAddMonthsClamped_ConvertsToUTCBeforeCounting(t *testing.T) {
	jerusalem, err := time.LoadLocation("Asia/Jerusalem")
	require.NoError(t, err)

	from := time.Date(2026, 8, 31, 1, 30, 0, 0, jerusalem)
	got := addMonthsClamped(from, 6)

	assert.Equal(t, time.Date(2027, 2, 28, 22, 30, 0, 0, time.UTC), got)
	assert.Equal(t, time.UTC, got.Location(), "result is expressed in UTC")
}

// The window has to hold whatever production computed from a clock reading
// between the two samples. Clamping is not monotonic, so the pair can arrive
// out of order — with the exact values from review below, the naive version
// produced a range nothing could satisfy.
func TestClampedWindow_SurvivesANonMonotonicBoundary(t *testing.T) {
	before := time.Date(2026, 8, 30, 23, 59, 59, 900_000_000, time.UTC)
	after := time.Date(2026, 8, 31, 0, 0, 0, 100_000_000, time.UTC)

	earliest, latest := clampedWindow(before, after, 6)

	require.False(t, latest.Before(earliest),
		"window is inverted: [%s .. %s]", earliest, latest)

	// Both instants production could have stamped fall inside it.
	for _, stamped := range []time.Time{before, after} {
		got := addMonthsClamped(stamped, 6)
		assert.False(t, got.Before(earliest), "%s excluded below the window", got)
		assert.False(t, got.After(latest), "%s excluded above the window", got)
	}
}

// The ordinary case is unaffected: no crossing, no swap.
func TestClampedWindow_KeepsOrderWhenNothingClamps(t *testing.T) {
	before := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	after := before.Add(time.Second)

	earliest, latest := clampedWindow(before, after, 6)

	assert.Equal(t, time.Date(2027, 2, 15, 12, 0, 0, 0, time.UTC), earliest)
	assert.Equal(t, time.Date(2027, 2, 15, 12, 0, 1, 0, time.UTC), latest)
}
