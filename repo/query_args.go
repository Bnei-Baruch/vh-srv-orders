package repo

import "fmt"

// queryArgs collects the bind values of a query that is assembled in pieces and
// hands back the placeholder naming each one.
//
// The WHERE builders below used to write their values straight into the SQL
// text with fmt.Sprintf. Most of those values arrive from request query
// parameters, and two of them from a Google Sheet cell, so a value of the right
// shape closed the quote and replaced the predicate rather than being compared
// against a column.
//
// Numbering has to be shared across the whole statement rather than restarted
// per fragment: the callers append their own LIMIT and OFFSET after the clause
// a builder returned, so a builder cannot assume it owns $1.
type queryArgs struct {
	values []any
}

// next records v and returns the placeholder that refers to it.
func (a *queryArgs) next(v any) string {
	a.values = append(a.values, v)
	return fmt.Sprintf("$%d", len(a.values))
}

// all returns the values in placeholder order, for passing to Query or Exec.
func (a *queryArgs) all() []any {
	return a.values
}

// limitOffset is the tail every listing query shares.
func (a *queryArgs) limitOffset(limit, skip int) string {
	return " LIMIT " + a.next(limit) + " OFFSET " + a.next(skip)
}
