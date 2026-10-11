package repo

import "strconv"

// queryArgs collects a query's bind arguments. arg adds a value and returns its
// placeholder, so request values reach Postgres as parameters, never as SQL.
type queryArgs []any

func (a *queryArgs) arg(v any) string {
	*a = append(*a, v)
	return "$" + strconv.Itoa(len(*a))
}
