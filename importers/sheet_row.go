package importers

import (
	"fmt"
	"strconv"
)

// cell reads one cell of a sheet row. The Sheets API omits trailing empty
// cells, so a row can be shorter than the sheet's columns, and row[i].(string)
// panics on it; a missing cell reads as "". Our Values.Get calls use the
// default FORMATTED_VALUE render, so cells arrive as strings; the float64 case
// covers UNFORMATTED_VALUE, which returns numbers as float64. It uses 'f'
// formatting because fmt.Sprint's %g turns 1e6 into "1e+06", which strconv
// then rejects.
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

// blankRow reports whether a row holds nothing at all. A spacer line is not a
// malformed row; counting it would report a drop on every run.
func blankRow(row []any) bool {
	for i := range row {
		if cell(row, i) != "" {
			return false
		}
	}
	return true
}
