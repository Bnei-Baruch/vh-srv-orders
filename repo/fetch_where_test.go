package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// updated_at is timestamptz. A bound written without its offset is read in the
// session time zone, so a +03:00 bound would land three hours off.
func TestOrdersWhere_DateBoundsKeepOffset(t *testing.T) {
	from := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("", 3*3600))
	to := time.Date(2026, 1, 2, 10, 0, 0, 0, time.FixedZone("", 3*3600))

	where, _, _ := buildAndGetOrdersWhereQuery(&from, &to,
		"", "", "", "", "", 0, "", "", "")

	assert.Contains(t, where, `o.updated_at <= '2026-01-02T10:00:00+03:00'`)
	assert.Contains(t, where, `o.updated_at >= '2026-01-01T10:00:00+03:00'`)
}

func TestPaymentsWhere_DateBoundsKeepOffset(t *testing.T) {
	from := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("", 3*3600))
	to := time.Date(2026, 1, 2, 10, 0, 0, 0, time.FixedZone("", 3*3600))

	where, _, _ := buildAndGetPaymentsWhereQuery(&from, &to,
		"", "", "", "", 0, "", nil, 0, "")

	assert.Contains(t, where, `p.updated_at <= '2026-01-02T10:00:00+03:00'`)
	assert.Contains(t, where, `p.updated_at >= '2026-01-01T10:00:00+03:00'`)
}
