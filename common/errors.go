package common

import "fmt"

var (
	ErrInvalidBody   = fmt.Errorf("invalid body")
	ErrInvalidValues = fmt.Errorf("invalid values")
)
