package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Orders get their userkey only from UpdateOrdersUserKeyFromAccounts, which the
// billing run calls before flagging. The flag endpoint did not, so an order
// created since the last run was never flagged (and before the repo fix, its
// NULL key panicked the request after the keyed users were flagged).
func TestOrdersFlag_ToRenew_SyncsUserKeysFirst(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	pool := a.testPool(t)
	ctx := context.Background()

	var accountID, orderID int
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO accounts ("Email", "UserKey") VALUES ('flag@test.test', 'kc-flag') RETURNING id`).Scan(&accountID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "Status", "ProductType", "Type", "PaymentDate", "Amount")
		 VALUES ($1, $2, $3, $4, $5, 100) RETURNING id`,
		accountID, common.OrderStatusPaid, common.ProductTypeGlobalMembership, common.OrderTypeRecurring,
		time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)).Scan(&orderID))

	got := POST_ROOT(t, a, "/orders/flag", gin.H{"flag": common.OrderFlagToRenew, "month": 6, "year": 2024}, http.StatusOK)
	assert.EqualValues(t, 1, got["count"])

	var flag *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, orderID).Scan(&flag))
	require.NotNil(t, flag)
	assert.Equal(t, common.OrderFlagToRenew, *flag)
}
