package importers

import "fmt"

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
func cell(row []any, i int) string {
	if i >= len(row) || row[i] == nil {
		return ""
	}
	if s, ok := row[i].(string); ok {
		return s
	}
	return fmt.Sprint(row[i])
}
