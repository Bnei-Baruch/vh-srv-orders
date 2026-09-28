package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func specialStarting(t time.Time) *repo.Special {
	return &repo.Special{StartDate: null.TimeFrom(t)}
}

func TestIsBeginsToday_StartsToday(t *testing.T) {
	assert.True(t, isBeginsToday(specialStarting(time.Now())))

	// Any time-of-day today counts — only the calendar date matters.
	//
	// Built with time.Date in the local zone, not time.Now().Truncate(24h):
	// Truncate rounds against the zero time in UTC, so east of Greenwich it
	// returns today's UTC midnight, which is 03:00 local in IDT. Between local
	// midnight and 03:00 that instant is still *yesterday* locally, and
	// isBeginsToday compares local calendar dates, so this assertion failed
	// for three hours every night. CI runs in UTC, where the two agree, so it
	// only ever failed on a developer's machine.
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	assert.True(t, isBeginsToday(specialStarting(midnight.Add(time.Minute))))
	assert.True(t, isBeginsToday(specialStarting(midnight)))

	// The other end of the same day.
	assert.True(t, isBeginsToday(specialStarting(midnight.AddDate(0, 0, 1).Add(-time.Second))))
}

// Regression: the old check compared today's day-of-month with itself, so any
// start date in the current year+month passed. A different day this month must not.
func TestIsBeginsToday_SameMonthOtherDay(t *testing.T) {
	now := time.Now()
	otherDay := now.AddDate(0, 0, 1)
	if otherDay.Month() != now.Month() { // today is month's last day — go backwards
		otherDay = now.AddDate(0, 0, -1)
	}
	assert.False(t, isBeginsToday(specialStarting(otherDay)))
}

func TestIsBeginsToday_YesterdayTomorrowAndFar(t *testing.T) {
	assert.False(t, isBeginsToday(specialStarting(time.Now().AddDate(0, 0, -1))))
	assert.False(t, isBeginsToday(specialStarting(time.Now().AddDate(0, 0, 1))))
	assert.False(t, isBeginsToday(specialStarting(time.Now().AddDate(-1, 0, 0)))) // same date last year
	assert.False(t, isBeginsToday(specialStarting(time.Now().AddDate(0, -1, 0)))) // same day last month
}

func TestIsBeginsToday_NoStartDate(t *testing.T) {
	assert.False(t, isBeginsToday(&repo.Special{}))
}
