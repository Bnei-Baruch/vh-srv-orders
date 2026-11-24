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

	mocks "github.com/Bnei-Baruch/vh-srv-orders/internal/mocks"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

func sheetRecord(email, keycloakID string) *SpecialRecord {
	return &SpecialRecord{
		Email:      null.NewString(email, email != ""),
		KeycloakID: null.NewString(keycloakID, keycloakID != ""),
		StartDate:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Category:   "membership",
	}
}

func storedSpecial(email, keycloakID string) *repo.Special {
	return &repo.Special{
		Email:      null.NewString(email, email != ""),
		KeycloakId: null.NewString(keycloakID, keycloakID != ""),
		// As the API writes it: a Jerusalem midnight, the previous UTC day.
		StartDate: null.TimeFrom(time.Date(2026, 1, 1, 0, 0, 0, 0, specialZone)),
		EndDate:   null.TimeFrom(time.Date(2026, 12, 31, 0, 0, 0, 0, specialZone)),
		Category:  null.StringFrom("membership"),
	}
}

// A rerun after fixing a few rows must not give the already-imported rows a second special.
func TestSpecialKeys_RecognisesAnAlreadyImportedRow(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")), "the same row is not imported twice")
}

// The sheet row carries only the email, so the match cannot come through the keycloak id.
func TestSpecialKeys_MatchesTheEmailCaseAndSpaceInsensitively(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	assert.True(t, keys.has(sheetRecord(" A@Example.COM ", "")))
}

// The process runs in UTC in prod; the key must still read the API's
// Jerusalem midnight as the day the sheet names.
func TestSpecialKeys_MatchesARowTheAPIWroteAtJerusalemMidnight(t *testing.T) {
	stored := storedSpecial("a@example.com", "kc-1")
	require.Equal(t, time.December, stored.StartDate.Time.UTC().Month(), "the fixture falls on the previous UTC day")

	keys := make(specialKeys)
	keys.addExisting(stored)
	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")))
}

func TestSpecialKeys_MatchesARowTheImporterWrote(t *testing.T) {
	stored := storedSpecial("a@example.com", "kc-1")
	stored.StartDate = null.TimeFrom(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stored.EndDate = null.TimeFrom(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))

	keys := make(specialKeys)
	keys.addExisting(stored)
	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")))
}

// Before dedup, a sheet row that moved the end date was imported; it still is.
func TestSpecialKeys_AMovedEndDateIsANewSpecial(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	moved := sheetRecord("a@example.com", "kc-1")
	moved.EndDate = time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
	assert.False(t, keys.has(moved))
}

// createSpecial may add a keycloak id the sheet never had, so a stored row
// must match through the identifier the sheet carries.
func TestSpecialKeys_MatchesOnTheIdentifierTheSheetCarries(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-resolved-at-import"))

	assert.True(t, keys.has(sheetRecord("a@example.com", "")), "the sheet line had no keycloak id; the stored row does")

	keys = make(specialKeys)
	keys.addExisting(storedSpecial("", "kc-only"))
	assert.True(t, keys.has(sheetRecord("", "kc-only")))
}

func TestSpecialKeys_DoesNotSwallowADifferentGrant(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	other := sheetRecord("b@example.com", "kc-2")
	assert.False(t, keys.has(other), "a different person is a different special")

	nextYear := sheetRecord("a@example.com", "kc-1")
	nextYear.StartDate = nextYear.StartDate.AddDate(1, 0, 0)
	nextYear.EndDate = nextYear.EndDate.AddDate(1, 0, 0)
	assert.False(t, keys.has(nextYear), "a renewal for the next window is a new special")

	otherCategory := sheetRecord("a@example.com", "kc-1")
	otherCategory.Category = "conference"
	assert.False(t, keys.has(otherCategory))

	withSub := sheetRecord("a@example.com", "kc-1")
	withSub.SubCategory = null.StringFrom("rav")
	assert.False(t, keys.has(withSub), "sub-category is part of what a special grants")
}

// A sheet that lists the same grant twice imports it once.
func TestSpecialKeys_SecondCopyWithinTheSameSheet(t *testing.T) {
	keys := make(specialKeys)
	row := sheetRecord("a@example.com", "kc-1")

	require.False(t, keys.has(row))
	keys.addImported(row)
	assert.True(t, keys.has(row))
}

// Revoking rewrites end_date; a key including it would re-grant revoked specials.
func TestSpecialKeys_ARevokedSpecialIsStillRecognised(t *testing.T) {
	keys := make(specialKeys)

	revoked := storedSpecial("a@example.com", "kc-1")
	// what the revoke's now() wrote: never a midnight
	revoked.EndDate = null.TimeFrom(time.Date(2026, 3, 15, 14, 23, 7, 512000, time.UTC))
	keys.addExisting(revoked)

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")),
		"the sheet row must not be re-imported over a revoke")
}

func TestSpecialKeys_AStoredRowWithNoEndIsStillRecognised(t *testing.T) {
	keys := make(specialKeys)

	stored := storedSpecial("a@example.com", "kc-1")
	stored.EndDate = null.Time{}
	keys.addExisting(stored)

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")))
}

func TestImportSpecialRecords_SkipsWhatTheTableHolds(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	var createdRows []string
	create := func(r *SpecialRecord) error {
		createdRows = append(createdRows, r.Email.String)
		return nil
	}

	created, skipped, failed := importSpecialRecords(
		[]*SpecialRecord{sheetRecord("a@example.com", "kc-1"), sheetRecord("b@example.com", "kc-2")}, keys, create)

	assert.Equal(t, []string{"b@example.com"}, createdRows)
	assert.Equal(t, 1, created)
	assert.Equal(t, 1, skipped)
	assert.Zero(t, failed)
}

func TestImportSpecialRecords_ASheetListingAGrantTwiceCreatesItOnce(t *testing.T) {
	calls := 0
	create := func(*SpecialRecord) error {
		calls++
		return nil
	}

	created, skipped, _ := importSpecialRecords(
		[]*SpecialRecord{sheetRecord("a@example.com", "kc-1"), sheetRecord("a@example.com", "kc-1")}, make(specialKeys), create)

	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, created)
	assert.Equal(t, 1, skipped)
}

func TestImportSpecialRecords_AFailedCreateIsCountedAndNotRemembered(t *testing.T) {
	calls := 0
	create := func(*SpecialRecord) error {
		calls++
		return errors.New("insert failed")
	}

	created, skipped, failed := importSpecialRecords(
		[]*SpecialRecord{sheetRecord("a@example.com", "kc-1"), sheetRecord("a@example.com", "kc-1")}, make(specialKeys), create)

	assert.Equal(t, 2, calls, "a row that failed is not treated as imported")
	assert.Zero(t, created)
	assert.Zero(t, skipped)
	assert.Equal(t, 2, failed)
}

// NULL and empty sub-categories are different stored values for the cleanup
// queries (`subcategory <> 'rav'`), so the key keeps them apart.
func TestSpecialKeys_NullAndEmptySubCategoryAreDifferentRows(t *testing.T) {
	keys := make(specialKeys)

	stored := storedSpecial("a@example.com", "kc-1") // SubCategory unset, i.e. NULL
	keys.addExisting(stored)

	sheet := sheetRecord("a@example.com", "kc-1")
	sheet.SubCategory = null.StringFrom("") // column present, cell blank

	assert.False(t, keys.has(sheet), "a blank cell is not the same stored value as NULL")
}

func TestSpecialKeys_MatchingSubCategoriesStillCollapse(t *testing.T) {
	keys := make(specialKeys)

	stored := storedSpecial("a@example.com", "kc-1")
	stored.SubCategory = null.StringFrom("rav")
	keys.addExisting(stored)

	sheet := sheetRecord("a@example.com", "kc-1")
	sheet.SubCategory = null.StringFrom("rav")

	assert.True(t, keys.has(sheet))
}

// existingSpecials derives the query window from the sheet's own rows.
func TestExistingSpecials_AsksForTheWindowTheSheetMentions(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	var gotFrom, gotTo time.Time
	mockRepo.EXPECT().GetSpecialsStartingBetween(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, from, to time.Time) ([]*repo.Special, error) {
			gotFrom, gotTo = from, to
			return nil, nil
		}).Once()

	early := sheetRecord("a@example.com", "")
	early.StartDate = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	late := sheetRecord("b@example.com", "")
	late.StartDate = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	_, err := im.existingSpecials(context.Background(), []*SpecialRecord{late, early})
	require.NoError(t, err)

	// A day either side of the sheet's own span, because the key compares local
	// dates and the bounds are instants.
	assert.Equal(t, early.StartDate.AddDate(0, 0, -1), gotFrom, "the window starts a day before the earliest row")
	assert.Equal(t, late.StartDate.AddDate(0, 0, 1), gotTo, "and ends a day after the latest")
}

// An empty sheet must not turn into an unbounded read.
func TestExistingSpecials_EmptySheetQueriesNothing(t *testing.T) {
	im := NewSpecialsImporter()
	im.repo = mocks.NewMockOrdersRepository(t) // no EXPECT: any call fails the test

	keys, err := im.existingSpecials(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestExistingSpecials_IndexesWhatTheRepoReturns(t *testing.T) {
	im := NewSpecialsImporter()
	mockRepo := mocks.NewMockOrdersRepository(t)
	im.repo = mockRepo

	stored := storedSpecial("a@example.com", "kc-1")
	mockRepo.EXPECT().GetSpecialsStartingBetween(mock.Anything, mock.Anything, mock.Anything).
		Return([]*repo.Special{stored}, nil).Once()

	keys, err := im.existingSpecials(context.Background(), []*SpecialRecord{sheetRecord("a@example.com", "kc-1")})
	require.NoError(t, err)
	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")))
}
