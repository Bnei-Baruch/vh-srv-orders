package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

// A held order leaves torenew, so a re-run of billing does not pick it up and
// charge it again while its first charge is unsettled.
func TestMarkRenewalChargePending_TakesTheOrderOutOfTorenew(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	require.NoError(t, db.MarkRenewalChargePending(ctx, uint(orderID)))

	order, err := db.GetOrderByID(ctx, uint(orderID))
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagChargePending, order.Flag.String)
	toRenew, err := db.GetOrderIDsToRenew(ctx)
	require.NoError(t, err)
	assert.NotContains(t, toRenew, uint(orderID))
}

func TestMarkRenewalChargePending_UnknownOrderIsNoRowsAffected(t *testing.T) {
	db, ctx := newTestDB(t)
	assert.ErrorIs(t, db.MarkRenewalChargePending(ctx, 999999), common.ErrNoRowsAffected)
}

// The held order's pending payment is listed once the cutoff has passed its
// creation, and not before.
func TestGetChargePendingRenewals_ListsHeldPaymentsOlderThanTheCutoff(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)
	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)
	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)
	require.NoError(t, db.MarkRenewalChargePending(ctx, uint(orderID)))

	pending, err := db.GetChargePendingRenewals(ctx, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []PendingCharge{{OrderID: uint(orderID), PaymentID: payment.ID}}, pending)

	tooEarly, err := db.GetChargePendingRenewals(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Empty(t, tooEarly)
}

// Only held orders, and of each only its newest pending payment: an older
// pending payment left by an earlier run is not the charge in question.
func TestGetChargePendingRenewals_OnlyHeldOrdersAndTheirNewestPendingPayment(t *testing.T) {
	db, ctx := newTestDB(t)
	accountID, heldID, _ := setupRenewalTestData(t, db, ctx)
	heldData, err := db.LoadRenewalData(ctx, uint(heldID))
	require.NoError(t, err)
	_, err = db.CreateRenewalPayment(ctx, heldData, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)
	newest, err := db.CreateRenewalPayment(ctx, heldData, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)
	require.NoError(t, db.MarkRenewalChargePending(ctx, uint(heldID)))

	// A torenew order with a pending payment (a run in progress) is not held.
	otherID := addRenewalOrder(t, db, ctx, accountID)
	otherData, err := db.LoadRenewalData(ctx, uint(otherID))
	require.NoError(t, err)
	_, err = db.CreateRenewalPayment(ctx, otherData, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)

	pending, err := db.GetChargePendingRenewals(ctx, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []PendingCharge{{OrderID: uint(heldID), PaymentID: newest.ID}}, pending)
}

// Settled as not charged, a held order goes back to torenew for the next run.
func TestFinalizeRenewal_DeclineReturnsAHeldOrderToTorenew(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)
	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)
	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)
	require.NoError(t, db.MarkRenewalChargePending(ctx, uint(orderID)))

	payment.PaymentStatus = null.StringFrom(common.PaymentStatusFailed)
	payment.Success = null.StringFrom("0")
	require.NoError(t, db.FinalizeRenewal(ctx, uint(orderID), payment))

	order, err := db.GetOrderByID(ctx, uint(orderID))
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, order.Flag.String)
	assert.Equal(t, common.OrderStatusNoSuccess, order.Status.String)
	pending, err := db.GetChargePendingRenewals(ctx, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, pending, "a settled payment is no longer pending")
}

// Settled as charged, a held order is renewed like any successful charge.
func TestFinalizeRenewal_SuccessRenewsAHeldOrder(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)
	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)
	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)
	require.NoError(t, db.MarkRenewalChargePending(ctx, uint(orderID)))

	payment.PaymentStatus = null.StringFrom(common.PaymentStatusSuccess)
	payment.Success = null.StringFrom("1")
	require.NoError(t, db.FinalizeRenewal(ctx, uint(orderID), payment))

	order, err := db.GetOrderByID(ctx, uint(orderID))
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagRenewed, order.Flag.String)
	assert.Equal(t, common.OrderStatusPaid, order.Status.String)
}

// The torenew reset applies to held orders only: a decline on an order with any
// other flag leaves the flag as it was.
func TestFinalizeRenewal_DeclineLeavesOtherFlagsAlone(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)
	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)
	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)
	require.NoError(t, db.FlagOrder(ctx, orderID, common.OrderFlagSkip))

	payment.PaymentStatus = null.StringFrom(common.PaymentStatusFailed)
	payment.Success = null.StringFrom("0")
	require.NoError(t, db.FinalizeRenewal(ctx, uint(orderID), payment))

	order, err := db.GetOrderByID(ctx, uint(orderID))
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagSkip, order.Flag.String)
}

// addRenewalOrder adds a second torenew recurring order with a successful
// payment to an existing account (one account per key, so setupRenewalTestData
// cannot run twice in a test).
func addRenewalOrder(t *testing.T, db *OrdersDB, ctx context.Context, accountID int) int {
	t.Helper()
	var orderID int
	createStr, numStr, args := prepareOrderCreateQuery(Order{
		AccountID: null.IntFrom(accountID), Amount: null.Float64From(80),
		Currency: null.StringFrom(common.CurrencyNIS), Type: null.StringFrom("recurring"),
		Status: null.StringFrom(common.OrderStatusPaid), Flag: null.StringFrom(common.OrderFlagToRenew),
	})
	require.NoError(t, db.pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createStr, numStr), args...).Scan(&orderID))
	createPStr, numPStr, pArgs := preparePaymentCreateQuery(Payment{
		OrderID: null.IntFrom(orderID), Amount: null.Float64From(80), Currency: null.StringFrom(common.CurrencyNIS),
		PelecardToken: null.StringFrom("TOKEN_DEF"), AuthNo: null.StringFrom("AUTH_456"),
		PaymentStatus: null.StringFrom(common.PaymentStatusSuccess), Success: null.StringFrom("1"),
	})
	var paymentID int
	require.NoError(t, db.pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPStr, numPStr), pArgs...).Scan(&paymentID))
	return orderID
}
