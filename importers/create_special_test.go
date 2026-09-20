package importers

import (
	"context"
	"errors"
	"testing"
	"time"

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
		Return(nil, errors.New("no such account")).Maybe()

	var got repo.Special
	mockRepo.EXPECT().CreateSpecial(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, s repo.Special) (int, error) {
			got = s
			return 1, nil
		}).Once()

	return &got
}

// specials.email is written by nothing else in production, and two readers scan
// it into a plain string: GetUniqueEmailsFromSpecial, which specialActivator
// calls before anything else, and DeleteSpecialById, on the revoke path. pgx
// refuses NULL into *string, so a single NULL-email row stopped specials
// activating for every user.
//
// The record keeps an unset Email — the guard and the account lookup read that
// — but the row written carries "".
func TestCreateSpecial_KeycloakOnlyRowStillWritesAnEmail(t *testing.T) {
	im := NewSpecialsImporter()
	got := captureCreatedSpecial(t, im)

	record := specialRecord()
	record.KeycloakID = null.StringFrom("kc-1")
	require.NoError(t, im.createSpecial(record))

	assert.True(t, got.Email.Valid, "an unset email must still be written as '', not NULL")
	assert.Empty(t, got.Email.String)
	assert.Equal(t, "kc-1", got.KeycloakId.String)
}

func TestCreateSpecial_EmailOnlyRowStillWritesAKeycloakID(t *testing.T) {
	im := NewSpecialsImporter()
	got := captureCreatedSpecial(t, im)

	record := specialRecord()
	record.Email = null.StringFrom("a@example.com")
	require.NoError(t, im.createSpecial(record))

	assert.Equal(t, "a@example.com", got.Email.String)
	assert.True(t, got.KeycloakId.Valid, "an unset keycloak id must still be written as ''")
	assert.Empty(t, got.KeycloakId.String)
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
