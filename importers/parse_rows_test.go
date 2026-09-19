package importers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An empty sheet used to panic here rather than import nothing: both parsers
// skip a header row with values[1:], and that slice expression is out of range
// when there is no header to skip.
func TestParseRows_EmptySheetDoesNotPanic(t *testing.T) {
	t.Run("specials", func(t *testing.T) {
		for _, values := range [][][]any{nil, {}} {
			records, err := parseSpecialRows(values)
			require.NoError(t, err)
			assert.Empty(t, records)
		}
	})

	t.Run("generic offline", func(t *testing.T) {
		for _, values := range [][][]any{nil, {}} {
			orders, err := parseGenericRows(values)
			require.NoError(t, err)
			assert.Empty(t, orders)
		}
	})
}

// The header-only case is what the live sheet returns today, and is the reason
// the empty case went unnoticed: one row in is already the safe side of the
// boundary.
func TestParseRows_HeaderOnly(t *testing.T) {
	records, err := parseSpecialRows([][]any{{"email", "keycloak_id", "start", "end", "category"}})
	require.NoError(t, err)
	assert.Empty(t, records)

	orders, err := parseGenericRows([][]any{{"email", "amount", "currency", "qty", "ts", "method", "comment"}})
	require.NoError(t, err)
	assert.Empty(t, orders)
}

// Guards the off-by-one the other way: the header must be skipped and the data
// row must not be.
func TestParseSpecialRows_SkipsHeaderKeepsData(t *testing.T) {
	records, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		{"b@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership", "sub"},
	})
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, "a@example.com", records[0].Email.String)
	assert.Equal(t, "kc-1", records[0].KeycloakID.String)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), records[0].StartDate)
	assert.Equal(t, "membership", records[0].Category)
	assert.False(t, records[0].SubCategory.Valid, "absent sixth column stays unset")
	assert.Equal(t, "sub", records[1].SubCategory.String)
}

func TestParseGenericRows_SkipsHeaderKeepsData(t *testing.T) {
	orders, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", "12.50", "USD", "2", "2026-01-01 10:00:00", "cash", "note"},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1)

	assert.Equal(t, "a@example.com", orders[0].Email)
	assert.InDelta(t, 12.50, orders[0].Amount, 0.001)
	assert.Equal(t, "USD", orders[0].Currency)
	assert.Equal(t, int64(2), orders[0].Quantity)
}

// A malformed row is skipped, not fatal — the importer logs and moves on, and
// the rows around it still land.
func TestParseGenericRows_SkipsMalformedRow(t *testing.T) {
	orders, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"bad@example.com", "12.50", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "5.00", "EUR", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1, "the unknown currency is dropped")
	assert.Equal(t, "ok@example.com", orders[0].Email)
}
