package repo

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

// The keycloak filter used to compare orders.userkey, a copy refreshed only by
// the monthly billing run, so an order created since was not found by its
// owner's key. It now goes through the order's account.
func TestGetAllOrders_KeycloakFilterFindsNewOrders(t *testing.T) {
	db, ctx := newTestDB(t)

	mine, err := db.CreateAccount(ctx, Account{UserKey: null.StringFrom("kc-1"), Email: null.StringFrom("a@test.test")})
	require.NoError(t, err)
	other, err := db.CreateAccount(ctx, Account{UserKey: null.StringFrom("kc-2"), Email: null.StringFrom("b@test.test")})
	require.NoError(t, err)
	var orderID int
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO orders ("AccountID") VALUES ($1) RETURNING id`, mine).Scan(&orderID))
	_, err = db.pool.Exec(ctx, `INSERT INTO orders ("AccountID") VALUES ($1)`, other)
	require.NoError(t, err)

	future := time.Now().Add(time.Hour)
	orders, err := db.GetAllOrders(ctx, 0, 10, "", &future, "", "", "", "", "", 0, "kc-1", "", "")
	require.NoError(t, err)
	require.Len(t, *orders, 1)
	assert.Equal(t, orderID, (*orders)[0].ID)
}

// Migration 30 drops orders.userkey; its down migration recreates the column
// from each order's account, leaving orders without an account NULL.
func TestMigration30_DropsOrdersUserkey_AndReverts(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()

	m, err := migrate.New("file://../db/migrations", dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })

	var accountID, keyed, orphan int
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO accounts ("UserKey") VALUES ('kc-1') RETURNING id`).Scan(&accountID))
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO orders ("AccountID") VALUES ($1) RETURNING id`, accountID).Scan(&keyed))
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO orders ("AccountID") VALUES (NULL) RETURNING id`).Scan(&orphan))

	var exists bool
	require.NoError(t, db.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'orders' AND column_name = 'userkey')`).Scan(&exists))
	require.False(t, exists, "test DB is at 30: the column is gone")

	require.NoError(t, m.Migrate(29))

	var key null.String
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, keyed).Scan(&key))
	assert.Equal(t, "kc-1", key.String)
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, orphan).Scan(&key))
	assert.False(t, key.Valid)
}
