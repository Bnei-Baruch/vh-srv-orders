package importers

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var genericHeader = []any{"email", "amount", "currency", "qty", "ts", "method", "comment"}

// values[1:] used to panic on a sheet with no rows at all.
func TestParseGenericRows_EmptySheetDoesNotPanic(t *testing.T) {
	for _, values := range [][][]any{nil, {}} {
		orders, dropped := parseGenericRows(values)
		assert.Empty(t, orders)
		assert.Zero(t, dropped)
	}
}

func TestParseGenericRows_HeaderOnly(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{genericHeader})
	assert.Empty(t, orders)
	assert.Zero(t, dropped)
}

func TestParseGenericRows_SkipsHeaderKeepsData(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"a@example.com", "12.50", "USD", "2", "2026-01-01 10:00:00", "cash", "note"},
	})
	require.Len(t, orders, 1)
	assert.Zero(t, dropped)

	assert.Equal(t, "a@example.com", orders[0].Email)
	assert.InDelta(t, 12.50, orders[0].Amount, 0.001)
	assert.Equal(t, "USD", orders[0].Currency)
	assert.Equal(t, 2, orders[0].Quantity)
}

func TestParseGenericRows_SkipsMalformedRow(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"bad@example.com", "12.50", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "5.00", "EUR", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", orders[0].Email)
}

// Every row bad is reported at Error level by reportDroppedRows; the parse
// itself returns normally, so the cron does not exit 1 on every run.
func TestParseGenericRows_EveryRowDroppedIsNotFatal(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"a@example.com", "12.50", "ILS", "1", "2026-01-01 10:00:00", "cash", ""},
		{"b@example.com", "5.00", "ILS", "1", "2026-01-01 11:00:00", "cash", ""},
	})
	assert.Empty(t, orders)
	assert.Equal(t, 2, dropped)
}

// The Sheets API omits trailing empty cells, so a blank last column arrives as
// a shorter row.
func TestParseGenericRows_TrailingCellsOmitted(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"a@example.com", "12.50", "USD", "1", "2026-01-01 10:00:00", "cash"},
		{"b@example.com", "5.00", "EUR", "2", "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped, "the row with no payment_method is dropped")
	assert.Empty(t, orders[0].Comment)
	assert.Equal(t, "cash", orders[0].PaymentMethod)
}

func TestParseGenericRows_NonStringCell(t *testing.T) {
	orders, _ := parseGenericRows([][]any{
		genericHeader,
		{"a@example.com", "12.5", "USD", float64(3), "2026-01-01 10:00:00", "cash", ""},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 3, orders[0].Quantity)
}

// fmt.Sprint renders float64(1e6) as "1e+06", which strconv rejects.
func TestParseGenericRows_LargeNumericCell(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"a@example.com", float64(1e6), "USD", float64(1e6), "2026-01-01 10:00:00", "cash", ""},
	})
	require.Len(t, orders, 1)
	assert.Zero(t, dropped)
	assert.Equal(t, 1000000, orders[0].Quantity)
	assert.InDelta(t, 1e6, orders[0].Amount, 0.001)

	assert.Equal(t, "12.5", cell([]any{float64(12.5)}, 0))
}

func TestParseGenericRows_WarnsWithSheetRowNumber(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	_, dropped := parseGenericRows([][]any{
		genericHeader,
		{"bad@example.com", "1.00", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	assert.Equal(t, 1, dropped)

	assert.Contains(t, buf.String(), "row=2", "the first data row is sheet row 2")
	assert.NotContains(t, buf.String(), "row=1 ")
}

// orders.quantity is int4. 4294967297 fits int64 but not int32, which is the
// range that tells a 32-bit parse from a 64-bit one.
func TestParseGenericRows_RejectsOversizedQuantity(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"big@example.com", "1.00", "USD", "4294967297", "2026-01-01 10:00:00", "cash"},
		{"ok@example.com", "1.00", "USD", "3", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", orders[0].Email)
	assert.Equal(t, 3, orders[0].Quantity)
}

func TestParseGenericRows_QuantityBoundary(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"max@example.com", "1.00", "USD", "2147483647", "2026-01-01 10:00:00", "cash"},
		{"over@example.com", "1.00", "USD", "2147483648", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, 2147483647, orders[0].Quantity)
}

func TestParseGenericRows_BlankSpacerRowsAreNotDrops(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"a@example.com", "12.50", "USD", "1", "2026-01-01 10:00:00", "cash"},
		{},
		{"", "", "", "", "", "", ""},
		{"b@example.com", "5.00", "EUR", "2", "2026-01-01 11:00:00", "cash"},
	})
	require.Len(t, orders, 2)
	assert.Zero(t, dropped)
}

func TestParseGenericRows_SkipsRowWithNoEmail(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"", "5.00", "USD", "1", "2026-01-01 10:00:00"},
		{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", orders[0].Email)
}

// Import iterates the filtered slice, so a survivor's index is not its sheet row.
func TestParseGenericRows_RecordCarriesItsSheetRow(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		genericHeader,
		{"bad@example.com", "1.00", "GBP", "1", "2026-01-01 10:00:00", "cash"},
		{"also@example.com", "1.00", "GBP", "1", "2026-01-01 10:00:00", "cash"},
		{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 2, dropped)
	assert.Equal(t, 4, orders[0].SheetRow)
}

func TestParseRobokasaRows_ShortAndNumericCells(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"rb-2", "b@example.com", "5.00"},
		{},
		{"rb-3", "c@example.com", float64(1e6), "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 2, "the short row is dropped; the numeric one is read")
	assert.Equal(t, 1, dropped, "the blank spacer is not counted")

	assert.Equal(t, "rb-1", orders[0].OrderID)
	assert.InDelta(t, 12.50, orders[0].Amount, 0.001)
	assert.Equal(t, "rb-3", orders[1].OrderID)
	assert.InDelta(t, 1e6, orders[1].Amount, 0.001)
}

func TestParseRobokasaRows_DropsRowWithNoOrderID(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"", "b@example.com", "5.00", "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, "rb-1", orders[0].OrderID)
	assert.Equal(t, 1, dropped)
}

func TestParseRobokasaRows_DropsRowWithNoEmail(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"rb-2", "", "5.00", "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, "rb-1", orders[0].OrderID)
	assert.Equal(t, 1, dropped)
}

func TestParseRobokasaRows_CarriesTheSheetRow(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"rb-2", "b@example.com", "not a number", "2026-01-01 11:00:00"},
		{"rb-3", "c@example.com", "7.00", "2026-01-01 12:00:00"},
	})
	require.Len(t, orders, 2)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, 1, orders[0].SheetRow)
	assert.Equal(t, 3, orders[1].SheetRow)
}
