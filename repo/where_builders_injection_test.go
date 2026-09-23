package repo

import (
	"context"
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

// predicateReplacing are values shaped to close the quote the WHERE builders
// used to wrap them in and take over the condition. Against an interpolating
// builder each one turns its filter into a tautology and the listing returns
// every row; bound as a parameter, each one is a string that matches nothing.
var predicateReplacing = []string{
	`x') OR 1=1 --`,
	`x') OR TRUE --`,
	`' OR ''='`,
	`x' OR 1=1 --`,
}

func seedOneOfEverything(t *testing.T, ctx context.Context, db *OrdersDB) {
	t.Helper()

	accountID, err := db.CreateAccount(ctx, Account{
		UserKey: null.StringFrom("keycloak-listing"),
		Email:   null.StringFrom("listing@example.com"),
	})
	require.Nil(t, err)

	orderID, err := db.CreateV2Order(ctx, Order{
		Type:          null.StringFrom(common.OrderTypeRegular),
		ProductType:   null.StringFrom(common.ProductTypeGlobalMembership),
		AccountID:     null.IntFrom(accountID),
		Amount:        null.Float64From(10),
		Currency:      null.StringFrom(common.CurrencyUSD),
		Status:        null.StringFrom(common.OrderStatusPaid),
		OrderLanguage: null.StringFrom(common.OrderLanguageEnglish),
		PaymentDate:   null.TimeFrom(time.Now()),
	})
	require.Nil(t, err)

	_, err = db.CreatePayment(ctx, RequestOrder{
		Amount:        null.Float64From(10),
		Currency:      null.StringFrom(common.CurrencyUSD),
		PaymentType:   null.StringFrom(common.PaymentTypeOffline),
		PaymentStatus: null.StringFrom(common.PaymentStatusSuccess),
		// An offline payment carries its own row, and payment_method there is
		// NOT NULL.
		PaymentMethod:        null.StringFrom("cash"),
		OfflinePaymentStatus: null.StringFrom(common.PaymentStatusSuccess),
	}, orderID)
	require.Nil(t, err)
}

// Every listing below takes its filters from request query parameters, and the
// accounts one is also reached from the sheet importers. The builders used to
// write those values into the SQL text with fmt.Sprintf, so a value of the
// right shape replaced the predicate instead of being compared against a
// column — a filtered listing then returned the whole table.
func TestWhereBuilders_FilterValuesAreBoundNotInterpolated(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.Nil(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.Nil(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())
	seedOneOfEverything(t, ctx, db)
	now := time.Now()

	for _, value := range predicateReplacing {
		t.Run("accounts by email", func(t *testing.T) {
			accounts, err := db.GetAllAccounts(ctx, 0, 50, value)
			require.NoError(t, err)
			assert.Empty(t, accounts, "%q must match no account", value)
		})

		t.Run("orders by email", func(t *testing.T) {
			orders, err := db.GetAllOrders(ctx, 0, 50, "", &now, "", "", "", "", value, 0, "", "", "")
			require.NoError(t, err)
			assert.Empty(t, *orders, "%q must match no order", value)
		})

		t.Run("orders by currency", func(t *testing.T) {
			orders, err := db.GetAllOrders(ctx, 0, 50, "", &now, "", value, "", "", "", 0, "", "", "")
			require.NoError(t, err)
			assert.Empty(t, *orders)
		})

		t.Run("orders by keycloak id", func(t *testing.T) {
			orders, err := db.GetAllOrders(ctx, 0, 50, "", &now, "", "", "", "", "", 0, value, "", "")
			require.NoError(t, err)
			assert.Empty(t, *orders)
		})

		t.Run("payments by status", func(t *testing.T) {
			payments, err := db.GetAllPayments(ctx, 0, 50, "", &now, "", value, "", "", 0, "", 0, "")
			require.NoError(t, err)
			assert.Empty(t, payments)
		})

		t.Run("payments by email", func(t *testing.T) {
			payments, err := db.GetAllPayments(ctx, 0, 50, "", &now, "", "", "", value, 0, "", 0, "")
			require.NoError(t, err)
			assert.Empty(t, payments)
		})

		t.Run("payment activities by email", func(t *testing.T) {
			// LIKE '%value%', so the comparison is a substring match either
			// way — what must not happen is the predicate being replaced.
			activities, err := db.GetPaymentActivities(ctx, value, "", "", 0, 50)
			require.NoError(t, err)
			assert.Empty(t, activities)
		})

		t.Run("participation count by product type", func(t *testing.T) {
			count, err := db.GetTotalParticipationStatusCount(ctx, "", value, "")
			require.NoError(t, err)
			assert.Zero(t, count)
		})

		t.Run("offline payments by method", func(t *testing.T) {
			payments, err := db.GetOfflinePayments(ctx, 0, 50, value, "")
			require.NoError(t, err)
			assert.Empty(t, payments)
		})
	}

	// The seeded row is still findable, so the assertions above mean "matched
	// nothing", not "the query is broken".
	accounts, err := db.GetAllAccounts(ctx, 0, 50, "listing@example.com")
	require.NoError(t, err)
	require.Len(t, accounts, 1)

	orders, err := db.GetAllOrders(ctx, 0, 50, "", &now, "", common.CurrencyUSD, "", "", "", 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, *orders, 1)

	// A value with a literal apostrophe is data, not syntax — the case a naive
	// escape breaks.
	quoted, err := db.CreateAccount(ctx, Account{Email: null.StringFrom(`o'brien@example.com`)})
	require.Nil(t, err)
	accounts, err = db.GetAllAccounts(ctx, 0, 50, `o'brien@example.com`)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, quoted, accounts[0].ID)
}
