package importers

import (
	"context"
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
		// Read back from timestamptz in the session's zone, which is not UTC.
		StartDate: null.TimeFrom(time.Date(2026, 1, 1, 2, 0, 0, 0, time.FixedZone("IDT", 2*60*60))),
		EndDate:   null.TimeFrom(time.Date(2026, 12, 31, 2, 0, 0, 0, time.FixedZone("IST", 2*60*60))),
		Category:  null.StringFrom("membership"),
	}
}

// A rerun after fixing a few rows must not give the already-imported rows a second special.
func TestSpecialKeys_RecognisesAnAlreadyImportedRow(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")), "the same row is not imported twice")
	assert.True(t, keys.has(sheetRecord("A@Example.com", "kc-1")), "the identifier is matched case-insensitively")
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
	revoked.EndDate = null.TimeFrom(time.Now()) // what the revoke wrote
	keys.addExisting(revoked)

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")),
		"the sheet row must not be re-imported over a revoke")
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
