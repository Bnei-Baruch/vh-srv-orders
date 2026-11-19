package repo

import (
	"strconv"
	"strings"
)

// queryArgs collects a query's bind arguments. arg adds a value and returns its
// placeholder, so request values reach Postgres as parameters, never as SQL.
type queryArgs []any

func (a *queryArgs) arg(v any) string {
	*a = append(*a, v)
	return "$" + strconv.Itoa(len(*a))
}

// colVals collects the columns a write sets, each with its value, in order.
// Column names come from code, never from a request; values are bound as $n.
type colVals struct {
	cols []string
	args queryArgs
}

func (c *colVals) add(col string, v any) {
	c.cols = append(c.cols, col)
	c.args = append(c.args, v)
}

func (c *colVals) empty() bool { return len(c.cols) == 0 }

// insertLists renders the column list and the matching "$1,$2,..." list of an
// INSERT, with their args.
func (c *colVals) insertLists() (string, string, queryArgs) {
	nums := make([]string, len(c.cols))
	for i := range c.cols {
		nums[i] = "$" + strconv.Itoa(i+1)
	}
	return strings.Join(c.cols, ","), strings.Join(nums, ","), c.args
}

// setList renders the "col=$1,col=$2,..." list of an UPDATE, with its args.
func (c *colVals) setList() (string, queryArgs) {
	sets := make([]string, len(c.cols))
	for i, col := range c.cols {
		sets[i] = col + "=$" + strconv.Itoa(i+1)
	}
	return strings.Join(sets, ","), c.args
}
