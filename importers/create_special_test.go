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

	mocks "github.com/Bnei-Baruch/vh-srv-orders/internal/mocks"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
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

// vh-srv-profile revokes by email, so a keycloak-only row is refused (#53).
func TestCreateSpecial_KeycloakOnlyRowIsRefused(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo
	mockRepo.EXPECT().GetAccount(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, pgx.ErrNoRows).Maybe()

	record := specialRecord()
	record.KeycloakID = null.StringFrom("kc-1")
	err := im.createSpecial(record)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email is required")
	mockRepo.AssertNotCalled(t, "CreateSpecial", mock.Anything, mock.Anything)
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

func TestCreateSpecial_RejectsRecordWithNeitherIdentifier(t *testing.T) {
	im := NewSpecialsImporter()
	im.repo = mocks.NewMockOrdersRepository(t)

	require.Error(t, im.createSpecial(specialRecord()))
}

// Account.UserKey is nullable; NULL must not replace the sheet's keycloak id,
// or DeleteSpecialsByKeycloakId can never revoke the special.
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

// An empty UserKey scans as Valid and must not replace the sheet's id either.
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

// A lookup failure other than "no such account" must surface: the dedup
// would otherwise skip the half-written row forever.
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
