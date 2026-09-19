package importers

import (
	"fmt"
	"strconv"
)

// cell reads one cell of a sheet row.
//
// The Sheets API omits trailing empty cells rather than padding the row, so a
// row whose last column is blank comes back short: a seven-column row with no
// comment arrives with length 6, and row[6] panics. Blank cells in the middle
// are returned as "", so only the tail goes missing — which is why this bites
// the optional columns that sit at the end.
//
// 179c5e5 patched one index this way after hitting it live. Every other index
// stayed exposed.
//
// A missing cell reads as empty, which is what it is. The value is rendered
// rather than type-asserted: a numeric or boolean cell arrives as float64 or
// bool, and .(string) on one of those panics the same way.
//
// float64 is formatted rather than handed to fmt.Sprint, which uses %g and so
// renders a quantity of one million as "1e+06". Every caller feeds the result
// to strconv, where that string is a malformed row. 'f' with precision -1 gives
// the shortest decimal form that round-trips, so 12.5 stays "12.5".
func cell(row []any, i int) string {
	if i >= len(row) || row[i] == nil {
		return ""
	}
	switch v := row[i].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// minRowsForFormatBreak is the smallest number of data rows from which "every
// one of them was dropped" says something about the sheet rather than about a
// row. Below it, a one-row sheet holding a single GBP donation would be read as
// a changed format — and the consequence is LogFatal, so the cron would die on
// every run until someone edited the sheet. A partial drop is reported to
// Sentry either way, so nothing goes unseen by keeping this floor.
const minRowsForFormatBreak = 2
