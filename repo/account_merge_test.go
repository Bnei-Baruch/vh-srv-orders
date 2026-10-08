package repo

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

const mergeMigrationVersion = 29

// accountRefs returns the account each seeded row points at.
func accountRefs(t *testing.T, db *OrdersDB, ctx context.Context, orderID, cardID, txID int) (int, int, int) {
	t.Helper()
	var o, c, x int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "AccountID" FROM orders WHERE id = $1`, orderID).Scan(&o))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT account_id FROM card_details WHERE id = $1`, cardID).Scan(&c))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT account_id FROM transaction WHERE id = $1`, txID).Scan(&x))
	return o, c, x
}

func seedAccountWithRows(t *testing.T, db *OrdersDB, ctx context.Context, key, email string) (accountID, orderID, cardID, txID int) {
	t.Helper()
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO accounts ("UserKey", "Email", created_at) VALUES ($1, $2, now()) RETURNING id`,
		null.NewString(key, key != ""), email).Scan(&accountID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", userkey) VALUES ($1, $2) RETURNING id`, accountID, key).Scan(&orderID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO card_details (cc_number, cc_expdate, account_id, active, token) VALUES ('4580', '1230', $1, true, 'tok') RETURNING id`,
		accountID).Scan(&cardID))
	var paymentID int
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO payments ("OrderID") VALUES ($1) RETURNING id`, orderID).Scan(&paymentID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO transaction (order_id, payment_id, account_id, terminal_id) VALUES ($1, $2, $3, (SELECT id FROM terminal LIMIT 1)) RETURNING id`,
		orderID, paymentID, accountID).Scan(&txID))
	return
}

// Migration 29 keeps each key's newest account, moves every row pointing at an
// older one to it, deletes the older ones and adds the unique index; its down
// migration puts every row and account back.
func TestMigration29_MergesDuplicateUserKeyAccounts_AndReverts(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()

	m, err := migrate.New("file://../db/migrations", dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	v, _, err := m.Version()
	require.NoError(t, err)
	require.EqualValues(t, mergeMigrationVersion, v, "test assumes 29 is the latest migration")
	require.NoError(t, m.Steps(-1))

	oldID, oldOrder, oldCard, oldTx := seedAccountWithRows(t, db, ctx, "kc-dup", "old@test.test")
	midID, midOrder, midCard, midTx := seedAccountWithRows(t, db, ctx, "kc-dup", "mid@test.test")
	newID, newOrder, newCard, newTx := seedAccountWithRows(t, db, ctx, "kc-dup", "new@test.test")
	soloID, soloOrder, soloCard, soloTx := seedAccountWithRows(t, db, ctx, "kc-solo", "solo@test.test")
	keylessA, _, _, _ := seedAccountWithRows(t, db, ctx, "", "a@test.test")
	keylessB, _, _, _ := seedAccountWithRows(t, db, ctx, "", "b@test.test")

	require.NoError(t, m.Steps(1))

	for _, id := range [][3]int{{oldOrder, oldCard, oldTx}, {midOrder, midCard, midTx}, {newOrder, newCard, newTx}} {
		o, c, x := accountRefs(t, db, ctx, id[0], id[1], id[2])
		assert.Equal(t, [3]int{newID, newID, newID}, [3]int{o, c, x}, "rows move to the newest account")
	}
	o, c, x := accountRefs(t, db, ctx, soloOrder, soloCard, soloTx)
	assert.Equal(t, [3]int{soloID, soloID, soloID}, [3]int{o, c, x}, "an unduplicated key is untouched")

	var n int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE id IN ($1, $2)`, oldID, midID).Scan(&n))
	assert.Zero(t, n, "older accounts are deleted")
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE id IN ($1, $2)`, keylessA, keylessB).Scan(&n))
	assert.Equal(t, 2, n, "accounts without a key are not merged with each other")

	id, err := db.GetAccountIDByKeycloakID(ctx, "kc-dup")
	require.NoError(t, err)
	assert.Equal(t, newID, id)

	_, err = db.pool.Exec(ctx, `INSERT INTO accounts ("UserKey") VALUES ('kc-dup')`)
	require.Error(t, err, "accounts_userkey_uniq refuses a second account for a key")

	require.NoError(t, m.Steps(-1))

	for _, tc := range []struct{ acct, order, card, tx int }{
		{oldID, oldOrder, oldCard, oldTx}, {midID, midOrder, midCard, midTx}, {newID, newOrder, newCard, newTx},
	} {
		o, c, x := accountRefs(t, db, ctx, tc.order, tc.card, tc.tx)
		assert.Equal(t, [3]int{tc.acct, tc.acct, tc.acct}, [3]int{o, c, x}, "down migration moves rows back")
	}
	var email string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email" FROM accounts WHERE id = $1`, midID).Scan(&email))
	assert.Equal(t, "mid@test.test", email, "down migration restores the deleted account verbatim")
}

// With accounts_userkey_uniq, two first requests for the same key race: both
// look the key up, find nothing, and both insert. The loser's insert hits the
// index; createAccountForKey must hand it the winner's account, not an error.
// Calling it for a key that already has an account is exactly the loser's state.
func TestCreateAccountForKey_LosingTheRaceReturnsTheWinner(t *testing.T) {
	db, ctx := newTestDB(t)

	a := Account{Email: null.StringFrom("race@test.test"), UserKey: null.StringFrom("kc-race")}
	winner, created, err := db.createAccountForKey(ctx, a)
	require.NoError(t, err)
	require.True(t, created)

	loser, created, err := db.createAccountForKey(ctx, a)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, winner, loser)

	var count int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE "UserKey" = 'kc-race'`).Scan(&count))
	assert.Equal(t, 1, count)
}
