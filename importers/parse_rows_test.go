package importers

import (
	"bytes"
	"log/slog"
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
	assert.Equal(t, 2, orders[0].Quantity)
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

// The Sheets API omits trailing empty cells instead of padding the row, so a
// blank last column arrives as a *shorter row*, not as "". The fixtures above
// spelled blanks explicitly, which is a shape the API never returns — so they
// proved a bad value was handled, never a missing cell.
//
// 179c5e5 hit this live and guarded one index. These rows are what it looked
// like.
func TestParseRows_TrailingCellsOmitted(t *testing.T) {
	t.Run("generic offline drops the comment column", func(t *testing.T) {
		orders, err := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			// seven columns declared, six present: no comment
			{"a@example.com", "12.50", "USD", "1", "2026-01-01 10:00:00", "cash"},
			// five present: no method either
			{"b@example.com", "5.00", "EUR", "2", "2026-01-01 11:00:00"},
		})
		require.NoError(t, err)
		require.Len(t, orders, 2)
		assert.Empty(t, orders[0].Comment)
		assert.Equal(t, "cash", orders[0].PaymentMethod)
		assert.Empty(t, orders[1].PaymentMethod)
		assert.Equal(t, 2, orders[1].Quantity)
	})

	t.Run("specials drops sub-category and category", func(t *testing.T) {
		records, err := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category", "sub"},
			// no sub-category
			{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
			// no category either — the index 179c5e5 left exposed
			{"b@example.com", "kc-2", "2026-02-01", "2026-11-30"},
		})
		require.NoError(t, err)
		require.Len(t, records, 2)
		assert.False(t, records[0].SubCategory.Valid)
		assert.Equal(t, "membership", records[0].Category)
		assert.Empty(t, records[1].Category)
	})
}

// A cell the sheet stores as a number arrives as float64, and .(string) on it
// panics exactly like a missing index does.
func TestParseRows_NonStringCell(t *testing.T) {
	orders, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", "12.5", "USD", float64(3), "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, 3, orders[0].Quantity, "a numeric cell is read, not panicked on")
}

// The header is sheet row 1, so the first data row is row 2. Reporting it as
// row 1 sends whoever is fixing the sheet to the wrong line.
func TestParseGenericRows_WarnsWithSheetRowNumber(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	_, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"bad@example.com", "1.00", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "row=2", "the first data row is sheet row 2, not 1")
	assert.NotContains(t, buf.String(), "row=1 ")
}

// A malformed date used to abort the whole import here, while the generic
// parser skipped the row and carried on. One bad cell should not stop every
// other row from landing.
func TestParseSpecialRows_SkipsBadDateAndContinues(t *testing.T) {
	records, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"bad@example.com", "kc-1", "not-a-date", "2026-12-31", "membership"},
		{"ok@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership"},
	})
	require.NoError(t, err)
	require.Len(t, records, 1, "the bad row is skipped, the good one still lands")
	assert.Equal(t, "ok@example.com", records[0].Email.String)
}

// A quantity too large for the destination used to be parsed at 64 bits and
// then narrowed to int, which truncates on a 32-bit build rather than
// rejecting: 4294967297 lands as 1. It is parsed at the destination's width
// now, so an out-of-range cell is a malformed row like any other.
func TestParseGenericRows_RejectsOversizedQuantity(t *testing.T) {
	orders, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"big@example.com", "1.00", "USD", "99999999999999999999", "2026-01-01 10:00:00", "cash"},
		{"ok@example.com", "1.00", "USD", "3", "2026-01-01 10:00:00", "cash"},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1, "the oversized quantity is a malformed row, not a truncated one")
	assert.Equal(t, "ok@example.com", orders[0].Email)
	assert.Equal(t, 3, orders[0].Quantity)
}
