package utils

import (
	"time"

	uuid "github.com/satori/go.uuid"
)

func PointerString(s string) *string {
	return &s
}

func PointerInt(i int) *int {
	return &i
}

func PointerBool(b bool) *bool {
	return &b
}

func PointerTime(t time.Time) *time.Time {
	return &t
}

func PointerUUID(id uuid.UUID) *uuid.UUID {
	return &id
}
