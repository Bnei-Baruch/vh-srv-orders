package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

// setupFlagTestDB creates a test database and returns the OrdersDB and a context with event builder.
func setupFlagTestDB(t *testing.T) (*OrdersDB, context.Context) {
	t.Helper()
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	ctx := eventstest.WithTestEventBuilder(t, context.Background())
	return db, ctx
}

// createTestAccount creates an account and returns its ID.
func createTestAccount(t *testing.T, db *OrdersDB, ctx context.Context, email string) int {
	t.Helper()
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom(email),
	})
	require.NoError(t, err)
	return accountID
}

// insertOrder inserts an order and sets its userkey. Returns the order ID.
func insertOrder(t *testing.T, db *OrdersDB, ctx context.Context, order Order, userkey string) int {
	t.Helper()
	createString, numString, args := prepareOrderCreateQuery(order)
	var id int
	err := db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		args...).Scan(&id)
	require.NoError(t, err)
	if userkey != "" {
		_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey, id)
		require.NoError(t, err)
	}
	return id
}

// renewableOrder returns an Order with the required fields for renewal eligibility.
func renewableOrder(accountID int, status string, paymentDate time.Time) Order {
	return Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		Status:      null.StringFrom(status),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Type:        null.StringFrom(common.OrderTypeRecurring),
		PaymentDate: null.TimeFrom(paymentDate),
	}
}

// getOrderFlag reads the Flag column for the given order ID.
func getOrderFlag(t *testing.T, db *OrdersDB, ctx context.Context, orderID int) string {
	t.Helper()
	var flag null.String
	err := db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, orderID).Scan(&flag)
	require.NoError(t, err)
	return flag.String
}

// ---------------------------------------------------------------------------
// FlagOrdersToRenew
// ---------------------------------------------------------------------------

func TestFlagOrdersToRenew_NoEligibleOrders(t *testing.T) {
	db, ctx := setupFlagTestDB(t)

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_SingleUserSingleOrder(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// Order with payment date in May, billing for June -> eligible
	orderID := insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, orderID))
}

func TestFlagOrdersToRenew_PaymentDateInBillingMonth_NotFlagged(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// Payment date is June 1 (first of billing month) -> NOT before billing date -> skip
	insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_PaymentDateAfterBillingMonth_NotFlagged(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// Payment date in July, billing for June -> future payment -> skip
	insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 7, 10, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_NoSuccessStatusEligible(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	orderID := insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusNoSuccess, time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, orderID))
}

func TestFlagOrdersToRenew_CancelledStatusNotEligible(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	insertOrder(t, db, ctx, Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		Status:      null.StringFrom(common.OrderStatusCancelled),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Type:        null.StringFrom(common.OrderTypeRecurring),
		PaymentDate: null.TimeFrom(time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
	}, "user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_RegularTypeNotEligible(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	insertOrder(t, db, ctx, Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		Status:      null.StringFrom(common.OrderStatusPaid),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Type:        null.StringFrom(common.OrderTypeRegular),
		PaymentDate: null.TimeFrom(time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
	}, "user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_DonationProductNotEligible(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	insertOrder(t, db, ctx, Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		Status:      null.StringFrom(common.OrderStatusPaid),
		ProductType: null.StringFrom(common.ProductTypeDonation),
		Type:        null.StringFrom(common.OrderTypeRecurring),
		PaymentDate: null.TimeFrom(time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
	}, "user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_MultipleUsers(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	acc1 := createTestAccount(t, db, ctx, "user1@test.com")
	acc2 := createTestAccount(t, db, ctx, "user2@test.com")
	acc3 := createTestAccount(t, db, ctx, "user3@test.com")

	id1 := insertOrder(t, db, ctx,
		renewableOrder(acc1, common.OrderStatusPaid, time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")
	id2 := insertOrder(t, db, ctx,
		renewableOrder(acc2, common.OrderStatusPaid, time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)),
		"user2-key")
	id3 := insertOrder(t, db, ctx,
		renewableOrder(acc3, common.OrderStatusNoSuccess, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)),
		"user3-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, id1))
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, id2))
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, id3))
}

func TestFlagOrdersToRenew_MultipleOrdersSameUser_OnlyMostRecent(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// Older order (January)
	olderID := insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")
	// Newer order (May) - this one should be selected (LIMIT 1, ORDER BY PaymentDate DESC)
	newerID := insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 5, 20, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, newerID))
	// Older order should NOT be flagged
	assert.NotEqual(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, olderID))
}

func TestFlagOrdersToRenew_MultipleOrdersSameUser_MostRecentInBillingMonth_NothingFlagged(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// Older order eligible by date
	insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")
	// Most recent order has payment date in billing month -> selected but skipped
	insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 6, 5, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestFlagOrdersToRenew_PaymentDateLastDayBeforeBillingMonth(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// May 31, 23:59:59 - just before June 1 -> should be flagged
	orderID := insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2024, 5, 31, 23, 59, 59, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, orderID))
}

func TestFlagOrdersToRenew_MixOfEligibleAndIneligible(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	acc1 := createTestAccount(t, db, ctx, "user1@test.com")
	acc2 := createTestAccount(t, db, ctx, "user2@test.com")
	acc3 := createTestAccount(t, db, ctx, "user3@test.com")
	acc4 := createTestAccount(t, db, ctx, "user4@test.com")

	// Eligible: paid, recurring, globalmembership, payment before billing month
	eligibleID := insertOrder(t, db, ctx,
		renewableOrder(acc1, common.OrderStatusPaid, time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	// Ineligible: cancelled status
	insertOrder(t, db, ctx, Order{
		AccountID:   null.IntFrom(acc2),
		Amount:      null.Float64From(100.0),
		Status:      null.StringFrom(common.OrderStatusCancelled),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Type:        null.StringFrom(common.OrderTypeRecurring),
		PaymentDate: null.TimeFrom(time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
	}, "user2-key")

	// Ineligible: regular type
	insertOrder(t, db, ctx, Order{
		AccountID:   null.IntFrom(acc3),
		Amount:      null.Float64From(100.0),
		Status:      null.StringFrom(common.OrderStatusPaid),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Type:        null.StringFrom(common.OrderTypeRegular),
		PaymentDate: null.TimeFrom(time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)),
	}, "user3-key")

	// Ineligible: payment date in billing month
	insertOrder(t, db, ctx,
		renewableOrder(acc4, common.OrderStatusPaid, time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC)),
		"user4-key")

	count, err := db.FlagOrdersToRenew(ctx, 6, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, eligibleID))
}

func TestFlagOrdersToRenew_JanuaryBilling(t *testing.T) {
	db, ctx := setupFlagTestDB(t)
	accountID := createTestAccount(t, db, ctx, "user1@test.com")

	// Payment date in December, billing for January -> eligible
	orderID := insertOrder(t, db, ctx,
		renewableOrder(accountID, common.OrderStatusPaid, time.Date(2023, 12, 15, 0, 0, 0, 0, time.UTC)),
		"user1-key")

	count, err := db.FlagOrdersToRenew(ctx, 1, 2024)

	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, common.OrderFlagToRenew, getOrderFlag(t, db, ctx, orderID))
}

// ---------------------------------------------------------------------------
// GetFlaggedOrders
// ---------------------------------------------------------------------------

func TestGetFlaggedOrders(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create an account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create orders with different flags
	order1 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(200.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)

	order3 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(300.0),
		Flag:      null.StringFrom("other"),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)

	// Fetch flagged orders
	orders, err := db.GetFlaggedOrders(ctx)
	require.NoError(t, err)

	// Should only return orders with Flag='torenew'
	assert.Len(t, orders, 2)
	orderIDs := make(map[int]bool)
	for _, order := range orders {
		orderIDs[order.ID] = true
		assert.Equal(t, common.OrderFlagToRenew, order.Flag.String)
	}
	assert.True(t, orderIDs[order1.ID])
	assert.True(t, orderIDs[order2.ID])
	assert.False(t, orderIDs[order3.ID])
}

func TestFlagOrder(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create an account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create an order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Update flag
	err = db.FlagOrder(ctx, order.ID, common.OrderFlagMuhHiyuvNiklat)
	require.NoError(t, err)

	// Verify flag was updated
	var flag null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order.ID).Scan(&flag)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagMuhHiyuvNiklat, flag.String)
}
