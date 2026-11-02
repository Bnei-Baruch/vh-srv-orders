package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"
)

func insertMergeAccount(t *testing.T, db *OrdersDB, ctx context.Context, key string, email null.String) int {
	t.Helper()
	var id int
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO accounts ("UserKey", "Email") VALUES ($1, $2) RETURNING id`, key, email).Scan(&id))
	return id
}

func insertMergeCard(t *testing.T, db *OrdersDB, ctx context.Context, accountID int, ccNumber string) int {
	t.Helper()
	var id int
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO card_details (cc_number, cc_expdate, account_id, active, token) VALUES ($1, '1230', $2, true, 'tok') RETURNING id`,
		ccNumber, accountID).Scan(&id))
	return id
}

func insertMergeOrder(t *testing.T, db *OrdersDB, ctx context.Context, accountID int, key string, cardID null.Int) int {
	t.Helper()
	var id int
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", userkey, card_details_id) VALUES ($1, $2, $3) RETURNING id`,
		accountID, key, cardID).Scan(&id))
	return id
}

// Every source card moves to the destination: one an order uses, one no order
// uses (a saved payment method), and one whose number the destination already
// has. The old merge deleted the last two, leaving the source's order pointing
// at a card_details row that no longer existed.
func TestMergeAccountsOrders_MovesEveryCard(t *testing.T) {
	db, ctx := newTestDB(t)

	src := insertMergeAccount(t, db, ctx, "kc-src", null.StringFrom("src@test.test"))
	dst := insertMergeAccount(t, db, ctx, "kc-dst", null.StringFrom("dst@test.test"))

	used := insertMergeCard(t, db, ctx, src, "4580")
	saved := insertMergeCard(t, db, ctx, src, "1111")
	shared := insertMergeCard(t, db, ctx, src, "9999")
	dstCard := insertMergeCard(t, db, ctx, dst, "9999")

	usedOrder := insertMergeOrder(t, db, ctx, src, "kc-src", null.IntFrom(used))
	sharedOrder := insertMergeOrder(t, db, ctx, src, "kc-src", null.IntFrom(shared))

	require.NoError(t, db.MergeAccountsOrders(ctx, AccountMergeRequest{
		SourceKeycloakID:      null.StringFrom("kc-src"),
		DestinationKeycloakID: null.StringFrom("kc-dst"),
	}))

	for _, card := range []int{used, saved, shared, dstCard} {
		var owner int
		require.NoError(t, db.pool.QueryRow(ctx, `SELECT account_id FROM card_details WHERE id = $1`, card).Scan(&owner),
			"card %d must still exist", card)
		assert.Equal(t, dst, owner, "card %d belongs to the destination", card)
	}

	for _, order := range []int{usedOrder, sharedOrder} {
		var account int
		var cardExists bool
		require.NoError(t, db.pool.QueryRow(ctx,
			`SELECT o."AccountID", EXISTS (SELECT 1 FROM card_details c WHERE c.id = o.card_details_id)
			 FROM orders o WHERE o.id = $1`, order).Scan(&account, &cardExists))
		assert.Equal(t, dst, account)
		assert.True(t, cardExists, "order %d must not point at a deleted card", order)
	}

	var n int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE id = $1`, src).Scan(&n))
	assert.Zero(t, n, "the source account is deleted")
}

// "Email" is nullable; merging an account without one used to fail scanning
// the NULL into a string.
func TestMergeAccountsOrders_SourceWithoutEmail(t *testing.T) {
	db, ctx := newTestDB(t)

	src := insertMergeAccount(t, db, ctx, "kc-src", null.String{})
	dst := insertMergeAccount(t, db, ctx, "kc-dst", null.StringFrom("dst@test.test"))
	order := insertMergeOrder(t, db, ctx, src, "kc-src", null.Int{})

	require.NoError(t, db.MergeAccountsOrders(ctx, AccountMergeRequest{
		SourceKeycloakID:      null.StringFrom("kc-src"),
		DestinationKeycloakID: null.StringFrom("kc-dst"),
	}))

	var account int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "AccountID" FROM orders WHERE id = $1`, order).Scan(&account))
	assert.Equal(t, dst, account)
}
