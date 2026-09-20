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

// blankRow reports whether a row holds nothing at all.
//
// The Sheets API returns an interior blank line as an empty array, and a row
// spaced out with empty cells the same way. Parsed, such a row fails whichever
// check comes first — currency in one importer, start_date in the other — so
// counting it as a dropped row makes an ordinary sheet layout report a drop on
// every run. A sheet laid out header / two donations / blank / two donations
// would report dropped=1 for as long as it keeps that shape, which is the kind
// of permanent alert that gets a Sentry rule muted and then hides the drops
// this reporting exists for.
func blankRow(row []any) bool {
	for i := range row {
		if cell(row, i) != "" {
			return false
		}
	}
	return true
}
