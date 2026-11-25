package importers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var specialHeader = []any{"email", "keycloak_id", "start", "end", "category", "sub", "comment"}

// values[1:] used to panic on a sheet with no rows at all.
func TestParseSpecialRows_EmptySheetDoesNotPanic(t *testing.T) {
	for _, values := range [][][]any{nil, {}} {
		records, dropped := parseSpecialRows(values)
		assert.Empty(t, records)
		assert.Zero(t, dropped)
	}
}

func TestParseSpecialRows_HeaderOnly(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{specialHeader})
	assert.Empty(t, records)
	assert.Zero(t, dropped)
}

func TestParseSpecialRows_SkipsHeaderKeepsData(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		{"b@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership", "sub"},
	})
	require.Len(t, records, 2)
	assert.Zero(t, dropped)

	assert.Equal(t, "a@example.com", records[0].Email.String)
	assert.Equal(t, "kc-1", records[0].KeycloakID.String)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), records[0].StartDate)
	assert.Equal(t, "membership", records[0].Category)
	assert.Empty(t, records[0].SubCategory.String)
	assert.Equal(t, "sub", records[1].SubCategory.String)
}

// A bad date used to abort the whole import.
func TestParseSpecialRows_SkipsBadDateAndContinues(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"bad@example.com", "kc-1", "not-a-date", "2026-12-31", "membership"},
		{"ok@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership"},
	})
	require.Len(t, records, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", records[0].Email.String)
}

func TestParseSpecialRows_EveryRowDroppedIsNotFatal(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"a@example.com", "kc-1", "01/01/2026", "31/12/2026", "membership"},
		{"b@example.com", "kc-2", "01/02/2026", "30/11/2026", "membership"},
	})
	assert.Empty(t, records)
	assert.Equal(t, 2, dropped)
}

func TestParseSpecialRows_SkipsMissingCategory(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31"},
		{"b@example.com", "kc-2", "2026-01-01", "2026-12-31", "", "sub"},
		{"ok@example.com", "kc-3", "2026-02-01", "2026-11-30", "membership"},
	})
	require.Len(t, records, 1)
	assert.Equal(t, 2, dropped)
	assert.Equal(t, "ok@example.com", records[0].Email.String)
}

// createSpecial refuses a row with no email, so the parser drops it and the
// drop report sees it; a keycloak id alone is not enough.
func TestParseSpecialRows_DropsRowWithNoEmail(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"", "", "2026-01-01", "2026-12-31", "membership"},
		{"", "kc-2", "2026-01-01", "2026-12-31", "membership", "x"},
		{"   ", "kc-3", "2026-01-01", "2026-12-31", "membership"},
		{"ok@example.com", "", "2026-02-01", "2026-11-30", "membership"},
	})
	require.Len(t, records, 1)
	assert.Equal(t, 3, dropped)

	assert.Equal(t, "ok@example.com", records[0].Email.String)
	assert.False(t, records[0].KeycloakID.Valid, "an empty cell leaves KeycloakID unset, not valid-and-empty")
}

// A trailing comment makes the row longer; it must not change how a blank
// sub-category is stored, or the dedup key changes with it.
func TestParseSpecialRows_BlankSubCategoryDoesNotDependOnRowLength(t *testing.T) {
	records, _ := parseSpecialRows([][]any{
		specialHeader,
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		{"b@example.com", "kc-2", "2026-01-01", "2026-12-31", "membership", "", "a note"},
	})
	require.Len(t, records, 2)

	assert.True(t, records[0].SubCategory.Valid, "a blank sub-category is stored as '', never NULL")
	assert.Empty(t, records[0].SubCategory.String)
	assert.Equal(t, records[0].SubCategory, records[1].SubCategory)
}

func TestParseSpecialRows_BlankSpacerRowsAreNotDrops(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		{},
		{"", "", "", "", ""},
		{"b@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership"},
	})
	require.Len(t, records, 2)
	assert.Zero(t, dropped)
}

func TestParseSpecialRows_RecordCarriesItsSheetRow(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		specialHeader,
		{"bad@example.com", "kc-1", "01/01/2026", "2026-12-31", "membership"},
		{"also@example.com", "kc-2", "02/01/2026", "2026-12-31", "membership"},
		{"ok@example.com", "kc-3", "2026-02-01", "2026-11-30", "membership"},
	})
	require.Len(t, records, 1)
	assert.Equal(t, 2, dropped)
	assert.Equal(t, 4, records[0].SheetRow)
}
