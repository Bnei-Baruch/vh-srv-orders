package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func specialStarting(t time.Time) *repo.Special {
	return &repo.Special{StartDate: null.TimeFrom(t)}
}

func TestIsBeginsToday_StartsToday(t *testing.T) {
	assert.True(t, isBeginsToday(specialStarting(time.Now())))
	// Any time-of-day today counts — only the calendar date matters.
	midnight := time.Now().Truncate(24 * time.Hour)
	assert.True(t, isBeginsToday(specialStarting(midnight.Add(time.Minute))))
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

type capturingEmitter struct{ events []events.Event }

func (e *capturingEmitter) Emit(_ context.Context, evs ...events.Event) {
	e.events = append(e.events, evs...)
}
func (e *capturingEmitter) Close(context.Context) {}

// A special created by email only has a NULL keycloak_id. vh-srv-profile asserts
// both identifiers to string, so the activation event must carry "", not null.
func TestDoTask_SpecialWithoutKeycloakIdActivatesWithEmptyString(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockRepo.EXPECT().GetUniqueEmailsFromSpecial(ctx).Return([]string{"a@example.com"}, nil)
	mockRepo.EXPECT().GetAllSpecialsByEmail(ctx, "a@example.com").Return([]*repo.Special{{
		Email:     null.StringFrom("a@example.com"),
		StartDate: null.TimeFrom(time.Now()),
		EndDate:   null.TimeFrom(time.Now().AddDate(0, 1, 0)),
	}}, nil)
	emitter := &capturingEmitter{}
	w := &Worker{repo: mockRepo, eventEmitter: emitter}

	require.NoError(t, w.DoTask())

	require.Len(t, emitter.events, 1)
	payload := emitter.events[0].Payload
	assert.Equal(t, events.TypeCreateSpecial, emitter.events[0].Type)
	assert.Equal(t, "a@example.com", payload["email"])
	assert.Equal(t, "", payload["keycloak_id"])
}
