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
			records, dropped := parseSpecialRows(values)
			assert.Empty(t, records)
			assert.Zero(t, dropped)
		}
	})

	t.Run("generic offline", func(t *testing.T) {
		for _, values := range [][][]any{nil, {}} {
			orders, dropped := parseGenericRows(values)
			assert.Empty(t, orders)
			assert.Zero(t, dropped)
		}
	})
}

// The header-only case is what the live sheet returns today, and is the reason
// the empty case went unnoticed: one row in is already the safe side of the
// boundary.
func TestParseRows_HeaderOnly(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{{"email", "keycloak_id", "start", "end", "category"}})
	assert.Empty(t, records)
	assert.Zero(t, dropped)

	orders, dropped := parseGenericRows([][]any{{"email", "amount", "currency", "qty", "ts", "method", "comment"}})
	assert.Empty(t, orders)
	assert.Zero(t, dropped)
}

// Guards the off-by-one the other way: the header must be skipped and the data
// row must not be.
func TestParseSpecialRows_SkipsHeaderKeepsData(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		{"b@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership", "sub"},
	})
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
	orders, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", "12.50", "USD", "2", "2026-01-01 10:00:00", "cash", "note"},
	})
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
	orders, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"bad@example.com", "12.50", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "5.00", "EUR", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	require.Len(t, orders, 1, "the unknown currency is dropped")
	assert.Equal(t, 1, dropped, "the dropped row is counted")
	assert.Equal(t, "ok@example.com", orders[0].Email)
}

// A sheet where every row is bad used to abort the import, which doImport turns
// into LogFatal — os.Exit(1). But "every row was dropped" is also true of two
// operators typing GBP, and the sheet is not cleared between runs, so the cron
// then exited 1 on every invocation until a human edited it. The event is
// reported at a higher Sentry level instead; the parse itself returns normally.
func TestParseRows_EveryRowDroppedIsNotFatal(t *testing.T) {
	t.Run("specials", func(t *testing.T) {
		records, dropped := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category"},
			// the whole column switched to DD/MM/YYYY
			{"a@example.com", "kc-1", "01/01/2026", "31/12/2026", "membership"},
			{"b@example.com", "kc-2", "01/02/2026", "30/11/2026", "membership"},
		})
		assert.Empty(t, records)
		assert.Equal(t, 2, dropped)
	})

	t.Run("generic offline", func(t *testing.T) {
		orders, dropped := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			{"a@example.com", "12.50", "ILS", "1", "2026-01-01 10:00:00", "cash", ""},
			{"b@example.com", "5.00", "ILS", "1", "2026-01-01 11:00:00", "cash", ""},
		})
		assert.Empty(t, orders)
		assert.Equal(t, 2, dropped)
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
		orders, dropped := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			// seven columns declared, six present: no comment
			{"a@example.com", "12.50", "USD", "1", "2026-01-01 10:00:00", "cash"},
			// five present: no method either
			{"b@example.com", "5.00", "EUR", "2", "2026-01-01 11:00:00"},
		})
		require.Len(t, orders, 2)
		assert.Zero(t, dropped)
		assert.Empty(t, orders[0].Comment)
		assert.Equal(t, "cash", orders[0].PaymentMethod)
		assert.Empty(t, orders[1].PaymentMethod)
		assert.Equal(t, 2, orders[1].Quantity)
	})

	t.Run("specials drops sub-category", func(t *testing.T) {
		records, dropped := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category", "sub"},
			// no sub-category
			{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
		})
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
	records, dropped := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category", "sub"},
		// the category cell is blank, so the row arrives four long
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31"},
		// present but empty, the mid-row shape the API does send
		{"b@example.com", "kc-2", "2026-01-01", "2026-12-31", "", "sub"},
		{"ok@example.com", "kc-3", "2026-02-01", "2026-11-30", "membership"},
	})
	require.Len(t, records, 1, "both category-less rows are dropped")
	assert.Equal(t, 2, dropped)
	assert.Equal(t, "ok@example.com", records[0].Email.String)
}

// A cell the sheet stores as a number arrives as float64, and .(string) on it
// panics exactly like a missing index does.
func TestParseRows_NonStringCell(t *testing.T) {
	orders, _ := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", "12.5", "USD", float64(3), "2026-01-01 10:00:00", "cash", ""},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 3, orders[0].Quantity, "a numeric cell is read, not panicked on")
}

// fmt.Sprint renders a float64 with %g, so a quantity of one million came back
// as "1e+06" and strconv then called the row malformed. Small numeric cells hid
// it: %g only reaches exponent form at 1e21 for values below, and at six digits
// for values like this one.
func TestParseRows_LargeNumericCell(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"a@example.com", float64(1e6), "USD", float64(1e6), "2026-01-01 10:00:00", "cash", ""},
	})
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
	_, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"bad@example.com", "1.00", "XYZ", "1", "2026-01-01 10:00:00", "cash", ""},
		{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash", ""},
	})
	assert.Equal(t, 1, dropped)

	assert.Contains(t, buf.String(), "row=2", "the first data row is sheet row 2, not 1")
	assert.NotContains(t, buf.String(), "row=1 ")
}

// A malformed date used to abort the whole import here, while the generic
// parser skipped the row and carried on. One bad cell should not stop every
// other row from landing.
func TestParseSpecialRows_SkipsBadDateAndContinues(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"bad@example.com", "kc-1", "not-a-date", "2026-12-31", "membership"},
		{"ok@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership"},
	})
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
	orders, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"big@example.com", "1.00", "USD", "4294967297", "2026-01-01 10:00:00", "cash"},
		{"ok@example.com", "1.00", "USD", "3", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1, "the oversized quantity is a malformed row, not a deferred insert failure")
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", orders[0].Email)
	assert.Equal(t, 3, orders[0].Quantity)
}

// The boundary itself: int4's maximum is a legal quantity, one past it is not.
func TestParseGenericRows_QuantityBoundary(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		{"max@example.com", "1.00", "USD", "2147483647", "2026-01-01 10:00:00", "cash"},
		{"over@example.com", "1.00", "USD", "2147483648", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, 2147483647, orders[0].Quantity)
}

// A blank line between entries comes back as an empty array, and every cell of
// it reads as "". Parsed, it fails whichever check comes first and would be
// counted as a dropped row — so a sheet laid out with spacer rows would report
// a drop on every run and page Sentry for a layout nobody needs to fix.
func TestParseRows_BlankSpacerRowsAreNotDrops(t *testing.T) {
	t.Run("generic offline", func(t *testing.T) {
		orders, dropped := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			{"a@example.com", "12.50", "USD", "1", "2026-01-01 10:00:00", "cash"},
			{},                           // the API's shape for a blank line
			{"", "", "", "", "", "", ""}, // and a row of empty cells
			{"b@example.com", "5.00", "EUR", "2", "2026-01-01 11:00:00", "cash"},
		})
		require.Len(t, orders, 2)
		assert.Zero(t, dropped, "a spacer row is not a malformed row")
	})

	t.Run("specials", func(t *testing.T) {
		records, dropped := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category"},
			{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership"},
			{},
			{"", "", "", "", ""},
			{"b@example.com", "kc-2", "2026-02-01", "2026-11-30", "membership"},
		})
		require.Len(t, records, 2)
		assert.Zero(t, dropped)
	})
}

// The generic importer attaches its order to an account looked up by email, and
// GetAccount with an empty one resolves to the most recently created account
// carrying no email — so a blank cell billed the order and its payment to an
// unrelated person, silently. The specials parser grew this guard a round
// earlier; this parser reaches the same lookup through getOrCreateAccount.
func TestParseGenericRows_SkipsRowWithNoEmail(t *testing.T) {
	orders, dropped := parseGenericRows([][]any{
		{"email", "amount", "currency", "qty", "ts", "method", "comment"},
		// blank email, trailing cells omitted — the shape the API sends
		{"", "5.00", "USD", "1", "2026-01-01 10:00:00"},
		{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash"},
	})
	require.Len(t, orders, 1, "a row with no email is dropped, not attached to whichever account has none")
	assert.Equal(t, 1, dropped)
	assert.Equal(t, "ok@example.com", orders[0].Email)
}

// null.StringFrom("") is Valid and Set, so filling Email and KeycloakID
// unconditionally made createSpecial's "can't both be empty" guard dead code.
// A row with neither identifier then reached GetAccount(ctx, 0, ""), which
// resolves to the most recent account carrying an empty email — and the special
// would be stamped with that person's UserKey.
func TestParseSpecialRows_SkipsRowWithNeitherIdentifier(t *testing.T) {
	records, dropped := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category"},
		{"", "", "2026-01-01", "2026-12-31", "membership"},
		{"", "kc-2", "2026-01-01", "2026-12-31", "membership"},
		{"ok@example.com", "", "2026-02-01", "2026-11-30", "membership"},
	})
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
	records, _ := parseSpecialRows([][]any{
		{"email", "keycloak_id", "start", "end", "category", "sub"},
		{"a@example.com", "kc-1", "2026-01-01", "2026-12-31", "membership", ""},
		{"b@example.com", "kc-2", "2026-01-01", "2026-12-31", "membership"},
	})
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
		orders, dropped := parseGenericRows([][]any{
			{"email", "amount", "currency", "qty", "ts", "method", "comment"},
			{"bad@example.com", "1.00", "GBP", "1", "2026-01-01 10:00:00", "cash"},  // sheet row 2
			{"also@example.com", "1.00", "GBP", "1", "2026-01-01 10:00:00", "cash"}, // sheet row 3
			{"ok@example.com", "1.00", "USD", "1", "2026-01-01 10:00:00", "cash"},   // sheet row 4
		})
		require.Len(t, orders, 1)
		assert.Equal(t, 2, dropped)
		assert.Equal(t, 4, orders[0].SheetRow, "index 0 of the filtered slice is sheet row 4")
	})

	t.Run("specials", func(t *testing.T) {
		records, dropped := parseSpecialRows([][]any{
			{"email", "keycloak_id", "start", "end", "category"},
			{"bad@example.com", "kc-1", "01/01/2026", "2026-12-31", "membership"},
			{"also@example.com", "kc-2", "02/01/2026", "2026-12-31", "membership"},
			{"ok@example.com", "kc-3", "2026-02-01", "2026-11-30", "membership"},
		})
		require.Len(t, records, 1)
		assert.Equal(t, 2, dropped)
		assert.Equal(t, 4, records[0].SheetRow)
	})
}

// The Robokasa export kept raw type assertions after the other two importers
// moved to cell(), so it carried both hazards sheet_row.go describes: a
// trailing blank shortens the row and row[3] panics, and a numeric cell arrives
// as float64 where .(string) panics. Either one killed the whole run.
//
// This sheet has no header, so row 1 is the first order.
func TestParseRobokasaRows_ShortAndNumericCells(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		// the timestamp column left blank: four columns declared, three sent
		{"rb-2", "b@example.com", "5.00"},
		{},
		// amount stored as a number, not text
		{"rb-3", "c@example.com", float64(1e6), "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 2, "the short row is dropped; the numeric one is read")
	assert.Equal(t, 1, dropped, "the blank spacer is not counted")

	assert.Equal(t, "rb-1", orders[0].OrderID)
	assert.InDelta(t, 12.50, orders[0].Amount, 0.001)
	assert.Equal(t, "rb-3", orders[1].OrderID)
	assert.InDelta(t, 1e6, orders[1].Amount, 0.001)
}

// Idempotency is keyed on OrderID. An imported blank id occupies the key "",
// so every later blank-id row matches it and is skipped forever as already
// imported — counted as skipped_orders, which reads like success.
func TestParseRobokasaRows_DropsRowWithNoOrderID(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"", "b@example.com", "5.00", "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 1, "a row that cannot name itself is malformed")
	assert.Equal(t, "rb-1", orders[0].OrderID)
	assert.Equal(t, 1, dropped)
}

// Same guard the other two parsers carry: getOrCreateAccount with an empty
// email resolves to whichever account was created last without one, so the
// order and its payment attach to an unrelated person, silently.
func TestParseRobokasaRows_DropsRowWithNoEmail(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"rb-2", "", "5.00", "2026-01-01 11:00:00"},
	})
	require.Len(t, orders, 1)
	assert.Equal(t, "rb-1", orders[0].OrderID)
	assert.Equal(t, 1, dropped)
}

// Import logs the row an operator opens to fix the export; its loop index
// counts survivors, short by every dropped and every already-imported row.
func TestParseRobokasaRows_CarriesTheSheetRow(t *testing.T) {
	orders, dropped := parseRobokasaRows([][]any{
		{"rb-1", "a@example.com", "12.50", "2026-01-01 10:00:00"},
		{"rb-2", "b@example.com", "not a number", "2026-01-01 11:00:00"},
		{"rb-3", "c@example.com", "7.00", "2026-01-01 12:00:00"},
	})
	require.Len(t, orders, 2)
	assert.Equal(t, 1, dropped)
	assert.Equal(t, 1, orders[0].SheetRow)
	assert.Equal(t, 3, orders[1].SheetRow, "the sheet row, not the index into the survivors")
}
