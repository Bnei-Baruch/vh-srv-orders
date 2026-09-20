package importers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/repo"
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

// A rerun after fixing a few rows used to give every already-imported person a
// second row: CreateSpecial always INSERTs, `specials` has no unique key, and
// the 197 good rows of a 200-row sheet are committed by the time the operator
// fixes the 3 bad ones.
func TestSpecialKeys_RecognisesAnAlreadyImportedRow(t *testing.T) {
	keys := make(specialKeys)
	keys.addExisting(storedSpecial("a@example.com", "kc-1"))

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")), "the same row is not imported twice")
	assert.True(t, keys.has(sheetRecord("A@Example.com", "kc-1")), "the identifier is matched case-insensitively")
}

// createSpecial replaces the keycloak id with the matched account's UserKey, so
// a row imported from an email-only sheet line comes back carrying an id the
// sheet never had. Matching on the identifier the sheet provides is what makes
// the second run recognise the first run's work.
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

// A sheet that lists the same person twice should import them once, without
// waiting for the next run to notice.
func TestSpecialKeys_SecondCopyWithinTheSameSheet(t *testing.T) {
	keys := make(specialKeys)
	row := sheetRecord("a@example.com", "kc-1")

	require.False(t, keys.has(row))
	keys.addImported(row)
	assert.True(t, keys.has(row))
}

// Revoking is a soft update that rewrites end_date (DeleteSpecialById:
// `SET end_date = now()`). A key that included that column stopped matching the
// sheet row the moment an admin revoked the grant, so the next hourly cron run
// re-inserted it — the revoke silently undone, the only trace new_specials=1.
func TestSpecialKeys_ARevokedSpecialIsStillRecognised(t *testing.T) {
	keys := make(specialKeys)

	revoked := storedSpecial("a@example.com", "kc-1")
	revoked.EndDate = null.TimeFrom(time.Now()) // what the revoke wrote
	keys.addExisting(revoked)

	assert.True(t, keys.has(sheetRecord("a@example.com", "kc-1")),
		"the sheet row must not be re-imported over a revoke")
}

// NULL and an empty sub-category are different stored values, and the cleanup
// queries compare the column — `where subcategory <> 'rav'` does not answer the
// same for both — so the key keeps them apart.
//
// The parser no longer produces NULL (a blank cell always stores the empty
// string, because len(row) depends on unrelated trailing columns), so this only
// separates a sheet row from an API-created one. It can cost a duplicate, never
// a missed grant.
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
