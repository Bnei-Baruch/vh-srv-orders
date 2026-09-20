package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	mocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// captureEmitter records what DoTask emits instead of publishing it.
type captureEmitter struct{ emitted []events.Event }

func (c *captureEmitter) Emit(_ context.Context, evs ...events.Event) {
	c.emitted = append(c.emitted, evs...)
}

func (c *captureEmitter) Close(context.Context) {}

func workerWithSpecials(t *testing.T, specials ...*repo.Special) (*Worker, *captureEmitter) {
	t.Helper()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockRepo.EXPECT().GetSpecialsStartingBetween(mock.Anything, mock.Anything, mock.Anything).
		Return(specials, nil).Once()

	emitter := &captureEmitter{}
	return &Worker{repo: mockRepo, eventEmitter: emitter}, emitter
}

func specialFor(keycloakID, email string, endsIn time.Duration) *repo.Special {
	return &repo.Special{
		Id:         null.IntFrom(1),
		KeycloakId: null.NewString(keycloakID, keycloakID != ""),
		Email:      null.StringFrom(email),
		StartDate:  null.TimeFrom(time.Now()),
		EndDate:    null.TimeFrom(time.Now().Add(endsIn)),
	}
}

func keycloakIDsOf(t *testing.T, emitted []events.Event) []string {
	t.Helper()
	var ids []string
	for _, e := range emitted {
		require.Equal(t, events.TypeCreateSpecial, e.Type)
		ids = append(ids, e.Payload["keycloak_id"].(null.String).String)
	}
	return ids
}

// The activator used to reach specials through the distinct emails in the
// table, so every keycloak-only special shared the key "" and was folded into a
// single person — the one with the latest end date. Everyone else beginning
// that day got nothing, and the next run no longer saw them as beginning today.
func TestDoTask_ActivatesEveryKeycloakOnlySpecialSeparately(t *testing.T) {
	w, emitter := workerWithSpecials(t,
		specialFor("kc-alice", "", 24*time.Hour),
		specialFor("kc-bob", "", 48*time.Hour),
	)

	require.NoError(t, w.DoTask())
	assert.ElementsMatch(t, []string{"kc-alice", "kc-bob"}, keycloakIDsOf(t, emitter.emitted))
}

// A special with a keycloak id and no email at all: handleCreateSpecial
// requires neither field, so these exist, and an email-keyed loop never saw
// them.
func TestDoTask_ActivatesASpecialWithNoEmail(t *testing.T) {
	special := specialFor("kc-carol", "", 24*time.Hour)
	special.Email = null.String{}
	w, emitter := workerWithSpecials(t, special)

	require.NoError(t, w.DoTask())
	assert.Equal(t, []string{"kc-carol"}, keycloakIDsOf(t, emitter.emitted))
}

// Folding to the longest span is the one part of the old loop that was right:
// two rows for the same person beginning today are one activation.
func TestDoTask_OnePersonWithTwoRowsActivatesOnceOnTheLongest(t *testing.T) {
	short := specialFor("kc-dave", "dave@example.com", 24*time.Hour)
	long := specialFor("kc-dave", "dave@example.com", 72*time.Hour)
	w, emitter := workerWithSpecials(t, short, long)

	require.NoError(t, w.DoTask())
	require.Len(t, emitter.emitted, 1)
	assert.Equal(t, long.EndDate, emitter.emitted[0].Payload["end_date"])
}

// Two people who share nothing but an email column are still two people when
// the email is the empty string the importer writes.
func TestDoTask_SkipsASpecialThatNamesNobody(t *testing.T) {
	orphan := specialFor("", "", 24*time.Hour)
	w, emitter := workerWithSpecials(t, orphan, specialFor("kc-erin", "", 24*time.Hour))

	require.NoError(t, w.DoTask())
	assert.Equal(t, []string{"kc-erin"}, keycloakIDsOf(t, emitter.emitted))
}

// Only today's specials activate. The read is deliberately a day wider than
// today so a timestamptz row near local midnight is not missed; isBeginsToday
// is what actually decides.
func TestDoTask_IgnoresSpecialsNotBeginningToday(t *testing.T) {
	tomorrow := specialFor("kc-frank", "", 24*time.Hour)
	tomorrow.StartDate = null.TimeFrom(time.Now().AddDate(0, 0, 1))
	w, emitter := workerWithSpecials(t, tomorrow)

	require.NoError(t, w.DoTask())
	assert.Empty(t, emitter.emitted)
}

// Neither column identifies a person on its own, so two rows that name the same
// human through different columns must not be two activations.
//
// Alice has one special created through the API (keycloak id and email) and one
// written by this importer before her account existed (email only). Keying on
// the keycloak id first put them in separate buckets and emitted both, so a
// last-write-wins consumer could truncate her longer grant to the shorter one.
func TestDoTask_OnePersonNamedTwoWaysIsOneActivation(t *testing.T) {
	withBoth := specialFor("kc-alice", "a@x.com", 30*24*time.Hour)
	emailOnly := specialFor("", "a@x.com", 365*24*time.Hour)
	w, emitter := workerWithSpecials(t, withBoth, emailOnly)

	require.NoError(t, w.DoTask())
	require.Len(t, emitter.emitted, 1, "one person, one activation")
	assert.Equal(t, emailOnly.EndDate, emitter.emitted[0].Payload["end_date"], "the longest window wins")
}

// Same person, reached the other way round: the row carrying both identifiers
// arrives after the one carrying only the id.
func TestDoTask_GroupsMergeWhicheverRowArrivesFirst(t *testing.T) {
	keycloakOnly := specialFor("kc-alice", "", 30*24*time.Hour)
	withBoth := specialFor("kc-alice", "a@x.com", 365*24*time.Hour)
	w, emitter := workerWithSpecials(t, keycloakOnly, withBoth)

	require.NoError(t, w.DoTask())
	require.Len(t, emitter.emitted, 1)
	assert.Equal(t, withBoth.EndDate, emitter.emitted[0].Payload["end_date"])
}

// Case-insensitively, because GetAllSpecialsByEmail matches with ilike.
func TestDoTask_EmailsGroupCaseInsensitively(t *testing.T) {
	w, emitter := workerWithSpecials(t,
		specialFor("", "A@X.com", 30*24*time.Hour),
		specialFor("", "a@x.com", 365*24*time.Hour),
	)

	require.NoError(t, w.DoTask())
	assert.Len(t, emitter.emitted, 1)
}

// Revoking is a soft update that moves end_date into the past, and it reaches
// rows that started at midnight today. Without an end_date check the next tick
// re-emits create_special for a special an admin revoked hours earlier,
// carrying an end_date that has already passed.
func TestDoTask_DoesNotActivateASpecialRevokedToday(t *testing.T) {
	revoked := specialFor("kc-alice", "", time.Hour)
	revoked.EndDate = null.TimeFrom(time.Now().Add(-time.Hour))
	w, emitter := workerWithSpecials(t, revoked)

	require.NoError(t, w.DoTask())
	assert.Empty(t, emitter.emitted, "a special that began and was ended is not one beginning today")
}
