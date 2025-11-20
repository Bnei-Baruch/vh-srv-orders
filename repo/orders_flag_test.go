package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

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
