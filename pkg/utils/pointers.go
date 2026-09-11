package utils

// Ptr returns a pointer to v, for the literals and function results that have
// no address of their own.
//
// Superseded by the language at Go 1.26, where new(expr) does this — delete
// this file and rewrite the calls when go.mod moves off 1.21.
func Ptr[T any](v T) *T {
	return &v
}
