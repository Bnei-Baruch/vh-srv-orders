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

// An order created since the last billing run is flagged through the endpoint:
// flagging reads the order's account, not a copied key that only the billing
// run refreshed.
func TestOrdersFlag_ToRenew_FlagsNewOrders(t *testing.T) {
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
