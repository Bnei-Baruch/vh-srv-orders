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

	where, _, args := buildAndGetOrdersWhereQuery(&from, &to,
		"", "", "", "", "", 0, "", "", "")

	// Bound, not formatted: the values carry their own offset to Postgres.
	assert.Contains(t, where, `o.updated_at <= $1 AND o.updated_at >= $2`)
	assert.Equal(t, []any{to, from}, args)
	assert.Equal(t, "+03:00", args[0].(time.Time).Format("-07:00"))
}

func TestPaymentsWhere_DateBoundsKeepOffset(t *testing.T) {
	from := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("", 3*3600))
	to := time.Date(2026, 1, 2, 10, 0, 0, 0, time.FixedZone("", 3*3600))

	where, _, args := buildAndGetPaymentsWhereQuery(&from, &to,
		"", "", "", "", 0, "", nil, 0, "")

	assert.Contains(t, where, `p.updated_at <= $1 AND p.updated_at >= $2`)
	assert.Equal(t, []any{to, from}, args)
	assert.Equal(t, "+03:00", args[0].(time.Time).Format("-07:00"))
}

// Ids are bound like every other value, never formatted into the SQL.
func TestPaymentsWhere_BindsIds(t *testing.T) {
	where, _, args := buildAndGetPaymentsWhereQuery(nil, &time.Time{},
		"", "", "", "", 7, "", nil, 9, "")

	assert.Contains(t, where, `a.id=$1`)
	assert.Contains(t, where, `p."OrderID" = $2`)
	assert.Equal(t, []any{7, 9}, args)
}

func TestOrdersWhere_BindsTheAccount(t *testing.T) {
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	where, _, args := buildAndGetOrdersWhereQuery(nil, &to,
		"", "", "", "", "", 7, "", "", "")

	assert.Contains(t, where, `o."AccountID" = $2`)
	assert.Equal(t, []any{to, 7}, args)
}
