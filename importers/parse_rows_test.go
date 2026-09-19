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
			records, dropped, err := parseSpecialRows(values)
			require.NoError(t, err)
			assert.Empty(t, records)
			assert.Zero(t, dropped)
		}
	})

	t.Run("generic offline", func(t *testing.T) {
		for _, values := range [][][]any{nil, {}} {
			orders, dropped, err := parseGenericRows(values)
			require.NoError(t, err)
			assert.Empty(t, orders)
			assert.Zero(t, dropped)
		}
	})
}

// The header-only case is what the live sheet returns today, and is the reason
// the empty case went unnoticed: one row in is already the safe side of the
// boundary.
func TestParseRows_HeaderOnly(t *testing.T) {
	records, dropped, err := parseSpecialRows([][]any{{"email", "keycloak_id", "start", "end", "category"}})
	require.NoError(t, err)
	assert.Empty(t, records)
	assert.Zero(t, dropped)

	orders, dropped, err := parseGenericRows([][]any{{"email", "amount", "currency", "qty", "ts", "method", "comment"}})
	require.NoError(t, err)
	assert.Empty(t, orders)
	assert.Zero(t, dropped)
}

// Guards the off-by-one the other way: the header must be skipped and the data
// row must not be.
func TestParseSpecialRows_SkipsHeaderKeepsData(t *testing.T) {
	records, dropped, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		{"b@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership", "sub"},
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Zero(t, dropped)

	assert.Equal(t, "a@example.com", records[0].Email.String)
	assert.Equal(t, "kc-1", records[0].KeycloakID.String)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), records[0].StartDate)
	assert.Equal(t, "membership", records[0].Category)
	assert.False(t, records[0].SubCategory.Valid, "absent sixth column stays unset")
	assert.Equal(t, "sub", records[1].SubCategory.String)
}

func TestParseGenericRows_SkipsHeaderKeepsData(t *testing.T) {
	orders, dropped, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", "12.50", "USD", "2", "2026-01-01 10:00:00", "cash", "note"},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Zero(t, dropped)

	assert.Equal(t, "a@example.com", orders[0].Email)
	assert.InDelta(t, 12.50, orders[0].Amount, 0.001)
	assert.Equal(t, "USD", orders[0].Currency)
	assert.Equal(t, 2, orders[0].Quantity)
}

// A malformed row is skipped, not fatal — the importer logs and moves on, and
// the rows around it still land. The drop is counted, not swallowed: a row the
// parser throws away never reaches createOrderAndPayments, so it appears in no
// summary total unless the parser reports it.
func TestParseGenericRows_SkipsMalformedRow(t *testing.T) {
	orders, dropped, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"bad@example.com", "12.50", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "5.00", "EUR", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1, "the unknown currency is dropped")
	assert.Equal(t, 1, dropped, "the dropped row is counted")
	assert.Equal(t, "ok@example.com", orders[0].Email)
}

// Dropping bad rows one at a time hides the case where the *sheet* broke: a
// changed date format or an inserted column drops every row, and the run then
// logs count=0 with_errors=0 and exits 0 — the same output as an empty sheet,
// and nothing reaches Sentry. All rows dropped is an error.
func TestParseRows_EveryRowDroppedIsAnError(t *testing.T) {
	t.Run("specials", func(t *testing.T) {
		records, dropped, err := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category"},
			// the whole column switched to DD/MM/YYYY
			{"a@example.com", "kc-1", "01/01/2026", "31/12/2026", "membership"},
			{"b@example.com", "kc-2", "01/02/2026", "30/11/2026", "membership"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "every data row was dropped")
		assert.Equal(t, 2, dropped)
		assert.Nil(t, records)
	})

	t.Run("generic offline", func(t *testing.T) {
		orders, dropped, err := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			{"a@example.com", "12.50", "ILS", "1", "2026-01-01 10:00:00", "cash", ""},
			{"b@example.com", "5.00", "ILS", "1", "2026-01-01 11:00:00", "cash", ""},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "every data row was dropped")
		assert.Equal(t, 2, dropped)
		assert.Nil(t, orders)
	})
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
		orders, dropped, err := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			// seven columns declared, six present: no comment
			{"a@example.com", "12.50", "USD", "1", "2026-01-01 10:00:00", "cash"},
			// five present: no method either
			{"b@example.com", "5.00", "EUR", "2", "2026-01-01 11:00:00"},
		})
		require.NoError(t, err)
		require.Len(t, orders, 2)
		assert.Zero(t, dropped)
		assert.Empty(t, orders[0].Comment)
		assert.Equal(t, "cash", orders[0].PaymentMethod)
		assert.Empty(t, orders[1].PaymentMethod)
		assert.Equal(t, 2, orders[1].Quantity)
	})

	t.Run("specials drops sub-category", func(t *testing.T) {
		records, dropped, err := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category", "sub"},
			// no sub-category
			{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		})
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Zero(t, dropped)
		assert.False(t, records[0].SubCategory.Valid)
		assert.Equal(t, "membership", records[0].Category)
	})
}

// A row short enough to lose its category used to be imported with an empty
// category.
// specials.category is varchar(50) NOT NULL and null.StringFrom("") is Valid,
// so the insert succeeds and the row grants nothing, silently. It is malformed.
func TestParseSpecialRows_SkipsMissingCategory(t *testing.T) {
	records, dropped, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category", "sub"},
		// the category cell is blank, so the row arrives four long
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31"},
		// present but empty, the mid-row shape the API does send
		{"b@example.com", "kc-2", "2026-01-01", "2026-12-31", "", "sub"},
		{"ok@example.com", "kc-3", "2026-02-01", "2026-11-30", "membership"},
	})
	require.NoError(t, err)
	require.Len(t, records, 1, "both category-less rows are dropped")
	assert.Equal(t, 2, dropped)
	assert.Equal(t, "ok@example.com", records[0].Email.String)
}

// A cell the sheet stores as a number arrives as float64, and .(string) on it
// panics exactly like a missing index does.
func TestParseRows_NonStringCell(t *testing.T) {
	orders, _, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", "12.5", "USD", float64(3), "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, 3, orders[0].Quantity, "a numeric cell is read, not panicked on")
}

// fmt.Sprint renders a float64 with %g, so a quantity of one million came back
// as "1e+06" and strconv then called the row malformed. Small numeric cells hid
// it: %g only reaches exponent form at 1e21 for values below, and at six digits
// for values like this one.
func TestParseRows_LargeNumericCell(t *testing.T) {
	orders, dropped, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", float64(1e6), "USD", float64(1e6), "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1, "1e+06 is not a malformed quantity")
	assert.Zero(t, dropped)
	assert.Equal(t, 1000000, orders[0].Quantity)
	assert.InDelta(t, 1e6, orders[0].Amount, 0.001)

	// The fractional case must survive the same formatting: 'f' with precision
	// -1 gives the shortest form that round-trips, not a padded one.
	assert.Equal(t, "12.5", cell([]any{float64(12.5)}, 0))
}

// The header is sheet row 1, so the first data row is row 2. Reporting it as
// row 1 sends whoever is fixing the sheet to the wrong line.
func TestParseGenericRows_WarnsWithSheetRowNumber(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	// A good row alongside it: every row dropped is an error, which is a
	// different path and would not exercise the warning.
	_, dropped, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"bad@example.com", "1.00", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, dropped)

	assert.Contains(t, buf.String(), "row=2", "the first data row is sheet row 2, not 1")
	assert.NotContains(t, buf.String(), "row=1 ")
}

// A malformed date used to abort the whole import here, while the generic
// parser skipped the row and carried on. One bad cell should not stop every
// other row from landing.
func TestParseSpecialRows_SkipsBadDateAndContinues(t *testing.T) {
	records, dropped, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"bad@example.com", "kc-1", "not-a-date", "2026-12-31", "membership"},
		{"ok@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership"},
	})
	require.NoError(t, err)
	require.Len(t, records, 1, "the bad row is skipped, the good one still lands")
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", records[0].Email.String)
}

// orders.quantity is int4. Parsing at Go's int width accepts anything up to
// 2^63 on the production 64-bit build and hands it to Postgres, which answers
// "4294967297 is greater than maximum value for int4" — a row that fails at
// insert time with the sheet already half imported.
//
// The value matters: an earlier version of this test used
// 99999999999999999999, which overflows int64 as well, so it passed against
// ParseInt(…, 64) too and pinned nothing. 4294967297 fits int64 and not int32,
// which is the only range that tells the two apart.
func TestParseGenericRows_RejectsOversizedQuantity(t *testing.T) {
	orders, dropped, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"big@example.com", "1.00", "USD", "4294967297", "2026-01-01 10:00:00", "cash"},
		{"ok@example.com", "1.00", "USD", "3", "2026-01-01 10:00:00", "cash"},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1, "the oversized quantity is a malformed row, not a deferred insert failure")
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", orders[0].Email)
	assert.Equal(t, 3, orders[0].Quantity)
}

// The boundary itself: int4's maximum is a legal quantity, one past it is not.
func TestParseGenericRows_QuantityBoundary(t *testing.T) {
	orders, dropped, err := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"max@example.com", "1.00", "USD", "2147483647", "2026-01-01 10:00:00", "cash"},
		{"over@example.com", "1.00", "USD", "2147483648", "2026-01-01 10:00:00", "cash"},
	})
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, 2147483647, orders[0].Quantity)
}

// "Every data row was dropped" is a statement about the sheet only when there
// were enough rows for it to be one. A sheet holding a header and a single GBP
// donation is not a changed format, and the consequence of calling it one is
// LogFatal: the cron dies on every run until someone edits the sheet.
func TestParseRows_SingleBadRowIsNotAFormatBreak(t *testing.T) {
	t.Run("generic offline", func(t *testing.T) {
		orders, dropped, err := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			{"a@example.com", "12.50", "GBP", "1", "2026-01-01 10:00:00", "cash"},
		})
		require.NoError(t, err, "one bad row out of one is a bad row, not a broken sheet")
		assert.Empty(t, orders)
		assert.Equal(t, 1, dropped)
	})

	t.Run("specials", func(t *testing.T) {
		records, dropped, err := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category"},
			{"a@example.com", "kc-1", "01/01/2026", "2026-12-31", "membership"},
		})
		require.NoError(t, err)
		assert.Empty(t, records)
		assert.Equal(t, 1, dropped)
	})
}

// null.StringFrom("") is Valid and Set, so filling Email and KeycloakID
// unconditionally made createSpecial's "can't both be empty" guard dead code.
// A row with neither identifier then reached GetAccount(ctx, 0, ""), which
// resolves to the most recent account carrying an empty email — and the special
// would be stamped with that person's UserKey.
func TestParseSpecialRows_SkipsRowWithNeitherIdentifier(t *testing.T) {
	records, dropped, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"", "", "2026-01-01", "2026-12-31", "membership"},
		{"", "kc-2", "2026-01-01", "2026-12-31", "membership"},
		{"ok@example.com", "", "2026-02-01", "2026-11-30", "membership"},
	})
	require.NoError(t, err)
	require.Len(t, records, 2, "only the row with neither identifier is dropped")
	assert.Equal(t, 1, dropped)

	assert.False(t, records[0].Email.Valid, "an empty email cell leaves Email unset, not valid-and-empty")
	assert.Equal(t, "kc-2", records[0].KeycloakID.String)
	assert.Equal(t, "ok@example.com", records[1].Email.String)
	assert.False(t, records[1].KeycloakID.Valid)
}

// A sub-category cell that is present and empty stored ” before the parser was
// rewritten. Storing NULL instead flips three-valued logic for the cleanup
// queries that compare it (`where subcategory <> 'rav'` never matches NULL), so
// present-and-empty and absent have to stay different.
func TestParseSpecialRows_PresentButEmptySubCategoryIsStored(t *testing.T) {
	records, _, err := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category", "sub"},
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership", ""},
		{"b@example.com", "kc-2", "2026-01-01", "2026-12-31", "membership"},
	})
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.True(t, records[0].SubCategory.Valid, "a present empty cell is stored as ''")
	assert.Empty(t, records[0].SubCategory.String)
	assert.False(t, records[1].SubCategory.Valid, "an absent cell stays NULL")
}

// The record carries its sheet row because the caller iterates the *filtered*
// slice: with rows dropped ahead of it, a survivor's index is no longer its
// line in the sheet, and an insert failure logged by index names the wrong row.
func TestParseRows_RecordCarriesItsSheetRow(t *testing.T) {
	t.Run("generic offline", func(t *testing.T) {
		orders, dropped, err := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			{"bad@example.com", "1.00", "GBP", "1", "2026-01-01 10:00:00", "cash"},  // sheet row 2
			{"also@example.com", "1.00", "GBP", "1", "2026-01-01 10:00:00", "cash"}, // sheet row 3
			{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash"},   // sheet row 4
		})
		require.NoError(t, err)
		require.Len(t, orders, 1)
		assert.Equal(t, 2, dropped)
		assert.Equal(t, 4, orders[0].SheetRow, "index 0 of the filtered slice is sheet row 4")
	})

	t.Run("specials", func(t *testing.T) {
		records, dropped, err := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category"},
			{"bad@example.com", "kc-1", "01/01/2026", "2026-12-31", "membership"},
			{"also@example.com", "kc-2", "02/01/2026", "2026-12-31", "membership"},
			{"ok@example.com", "kc-3", "2026-02-01", "2026-11-30", "membership"},
		})
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, 2, dropped)
		assert.Equal(t, 4, records[0].SheetRow)
	})
}
