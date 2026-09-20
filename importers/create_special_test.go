package importers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	mocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func specialRecord() *SpecialRecord {
	return &SpecialRecord{
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Category:  "membership",
		SheetRow:  2,
	}
}

func captureCreatedSpecial(t *testing.T, im *SpecialsImporter) *repo.Special {
	t.Helper()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	mockRepo.EXPECT().GetAccount(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, pgx.ErrNoRows).Maybe()

	var got repo.Special
	mockRepo.EXPECT().CreateSpecial(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, s repo.Special) (int, error) {
			got = s
			return 1, nil
		}).Once()

	return &got
}

// An identifier the sheet does not carry is left unset, so the column inserts
// NULL rather than an empty string.
//
// The empty string was written deliberately for a while, because
// GetUniqueEmailsFromSpecial and DeleteSpecialById scanned the column into a
// plain string and pgx refuses NULL there. Both readers are gone or fixed, and
// the sentinel turned out not to be inert: DeleteAccount and MergeAccounts
// delete specials by `email = (SELECT "Email" FROM accounts WHERE id = $1)`, so
// an account whose own email is empty matched every keycloak-only grant in the
// table.
func TestCreateSpecial_KeycloakOnlyRowLeavesTheEmailNull(t *testing.T) {
	im := NewSpecialsImporter()
	got := captureCreatedSpecial(t, im)

	record := specialRecord()
	record.KeycloakID = null.StringFrom("kc-1")
	require.NoError(t, im.createSpecial(record))

	assert.False(t, got.Email.Valid, "an identifier the sheet did not supply must insert NULL, not ''")
	assert.Equal(t, "kc-1", got.KeycloakId.String)
}

func TestCreateSpecial_EmailOnlyRowLeavesTheKeycloakIdNull(t *testing.T) {
	im := NewSpecialsImporter()
	got := captureCreatedSpecial(t, im)

	record := specialRecord()
	record.Email = null.StringFrom("a@example.com")
	require.NoError(t, im.createSpecial(record))

	assert.Equal(t, "a@example.com", got.Email.String)
	assert.False(t, got.KeycloakId.Valid, "no account matched, so there is no id to write")
}

// A record with neither identifier never reaches here — the parser drops it —
// but the guard is what says so, and it only works because the record's own
// fields stay unset.
func TestCreateSpecial_RejectsRecordWithNeitherIdentifier(t *testing.T) {
	im := NewSpecialsImporter()
	im.repo = mocks.NewMockOrdersRepository(t)

	require.Error(t, im.createSpecial(specialRecord()))
}

// Account.UserKey is nullable — accounts predating the profile-service path
// have it NULL. Clobbering a valid id with an invalid one drops the column from
// the insert, so keycloak_id lands NULL and DeleteSpecialsByKeycloakId
// (keycloak_id = $1) can never revoke the special.
func TestCreateSpecial_AccountWithoutAKeyDoesNotClobberTheSheetsID(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	mockRepo.EXPECT().GetAccount(mock.Anything, 0, "a@example.com").
		Return(&repo.Account{ID: 7}, nil).Once()

	var got repo.Special
	mockRepo.EXPECT().CreateSpecial(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, s repo.Special) (int, error) {
			got = s
			return 1, nil
		}).Once()

	record := specialRecord()
	record.Email = null.StringFrom("a@example.com")
	record.KeycloakID = null.StringFrom("kc-from-sheet")
	require.NoError(t, im.createSpecial(record))

	assert.True(t, got.KeycloakId.Valid, "an account with no UserKey must not make this NULL")
	assert.Equal(t, "kc-from-sheet", got.KeycloakId.String)
}

// The lookup still does its job when the account has a key — that is how an
// email-only row acquires one.
func TestCreateSpecial_AccountKeyResolvesAnEmailOnlyRow(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	mockRepo.EXPECT().GetAccount(mock.Anything, 0, "a@example.com").
		Return(&repo.Account{ID: 7, UserKey: null.StringFrom("kc-resolved")}, nil).Once()

	var got repo.Special
	mockRepo.EXPECT().CreateSpecial(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, s repo.Special) (int, error) {
			got = s
			return 1, nil
		}).Once()

	record := specialRecord()
	record.Email = null.StringFrom("a@example.com")
	require.NoError(t, im.createSpecial(record))

	assert.Equal(t, "kc-resolved", got.KeycloakId.String)
}

// accounts."UserKey" is a plain nullable text column: a row holding the empty
// string scans as Valid. Overwriting the sheet's id with that has the same
// result as NULL — DeleteSpecialsByKeycloakId (keycloak_id = $1) can never
// match the row again.
func TestCreateSpecial_AccountWithAnEmptyKeyDoesNotClobberTheSheetsID(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	mockRepo.EXPECT().GetAccount(mock.Anything, 0, "a@example.com").
		Return(&repo.Account{ID: 7, UserKey: null.StringFrom("")}, nil).Once()

	var got repo.Special
	mockRepo.EXPECT().CreateSpecial(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, s repo.Special) (int, error) {
			got = s
			return 1, nil
		}).Once()

	record := specialRecord()
	record.Email = null.StringFrom("a@example.com")
	record.KeycloakID = null.StringFrom("kc-from-sheet")
	require.NoError(t, im.createSpecial(record))

	assert.Equal(t, "kc-from-sheet", got.KeycloakId.String)
}

// A GetAccount failure that is not "no such account" has to surface.
//
// The lookup used to swallow every error, which was self-correcting by accident
// while the importer was not idempotent: the next tick re-inserted the row and
// the account usually existed by then. With the dedup index in place the
// half-written row is recognised as already present on every later run, so a
// transient database error leaves a special that can never be revoked by
// keycloak id and never gets retried.
func TestCreateSpecial_ATransientAccountLookupFailureIsNotSwallowed(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	mockRepo.EXPECT().GetAccount(mock.Anything, 0, "a@example.com").
		Return(nil, errors.New("connection reset by peer")).Once()

	record := specialRecord()
	record.Email = null.StringFrom("a@example.com")
	record.KeycloakID = null.StringFrom("kc-1")

	require.Error(t, im.createSpecial(record), "the row must be retried, not written without its keycloak id")
}
