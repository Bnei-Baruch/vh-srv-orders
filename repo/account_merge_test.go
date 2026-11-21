package repo

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/testutil"
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
	require.NoError(t, m.Migrate(mergeMigrationVersion-1))

	oldID, oldOrder, oldCard, oldTx := seedAccountWithRows(t, db, ctx, "kc-dup", "old@test.test")
	midID, midOrder, midCard, midTx := seedAccountWithRows(t, db, ctx, "kc-dup", "mid@test.test")
	newID, newOrder, newCard, newTx := seedAccountWithRows(t, db, ctx, "kc-dup", "new@test.test")
	soloID, soloOrder, soloCard, soloTx := seedAccountWithRows(t, db, ctx, "kc-solo", "solo@test.test")
	keylessA, _, _, _ := seedAccountWithRows(t, db, ctx, "", "a@test.test")
	keylessB, _, _, _ := seedAccountWithRows(t, db, ctx, "", "b@test.test")

	require.NoError(t, m.Migrate(mergeMigrationVersion))

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

	// A move made after the merge must survive the down migration.
	third, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-third", "third@test.test")
	_, err = db.pool.Exec(ctx, `UPDATE orders SET "AccountID" = $1 WHERE id = $2`, third, midOrder)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(mergeMigrationVersion-1))

	var midOrderAccount int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "AccountID" FROM orders WHERE id = $1`, midOrder).Scan(&midOrderAccount))
	assert.Equal(t, third, midOrderAccount, "a row moved after the merge stays where it was put")

	for _, tc := range []struct{ acct, order, card, tx int }{
		{oldID, oldOrder, oldCard, oldTx}, {newID, newOrder, newCard, newTx},
	} {
		o, c, x := accountRefs(t, db, ctx, tc.order, tc.card, tc.tx)
		assert.Equal(t, [3]int{tc.acct, tc.acct, tc.acct}, [3]int{o, c, x}, "down migration moves rows back")
	}
	_, c, x = accountRefs(t, db, ctx, midOrder, midCard, midTx)
	assert.Equal(t, [2]int{midID, midID}, [2]int{c, x}, "down migration moves rows back")
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

// PerformOperation moving an account onto a keycloak id that already has one
// would give that key two accounts. It must refuse before changing anything.
func TestPerformOperation_RefusesKeyThatHasAnAccount(t *testing.T) {
	db, ctx := newTestDB(t)

	oldID, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-old", "old@test.test")
	seedAccountWithRows(t, db, ctx, "kc-new", "new@test.test")

	str := func(s string) *string { return &s }
	_, err := db.PerformOperation(ctx, OperationReq{
		OldKeycloakID: str("kc-old"), NewKeycloakID: str("kc-new"),
		OldEmail: str("old@test.test"), NewEmail: str("new@test.test"),
	})
	require.ErrorIs(t, err, common.ErrAccountKeyTaken)

	var key string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "UserKey" FROM accounts WHERE id = $1`, oldID).Scan(&key))
	assert.Equal(t, "kc-old", key)
}

// seedMergeCopy records loser as merged into winner, the way migration 29 does.
func seedMergeCopy(t *testing.T, db *OrdersDB, ctx context.Context, loserID, winnerID int) {
	t.Helper()
	_, err := db.pool.Exec(ctx, `INSERT INTO account_merges (loser_id, winner_id, loser)
		VALUES ($1, $2, '{"Email": "loser@test.test", "PaymentToken": "tok"}')`, loserID, winnerID)
	require.NoError(t, err)
	_, err = db.pool.Exec(ctx, `INSERT INTO account_merge_moves (tbl, row_id, loser_id) VALUES ('orders', $1, $1)`, loserID)
	require.NoError(t, err)
}

func mergeCopies(t *testing.T, db *OrdersDB, ctx context.Context, loserID int) (merges, moves int, winnerID null.Int) {
	t.Helper()
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*), max(winner_id) FROM account_merges WHERE loser_id = $1`, loserID).
		Scan(&merges, &winnerID))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM account_merge_moves WHERE loser_id = $1`, loserID).Scan(&moves))
	return merges, moves, winnerID
}

// account_merges holds a full copy of each account migration 29 deleted. A
// member's "delete my data" must remove the copies merged into their account.
func TestHardDelete_RemovesTheAccountsMergeCopies(t *testing.T) {
	db, ctx := newTestDB(t)

	winner, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-winner", "winner@test.test")
	other, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-other", "other@test.test")
	seedMergeCopy(t, db, ctx, 900001, winner)
	seedMergeCopy(t, db, ctx, 900002, other)

	require.NoError(t, db.HardDeleteAllUserDataByAccountID(ctx, winner, ""))

	merges, moves, _ := mergeCopies(t, db, ctx, 900001)
	assert.Zero(t, merges)
	assert.Zero(t, moves)
	merges, moves, _ = mergeCopies(t, db, ctx, 900002)
	assert.Equal(t, 1, merges, "another member's copy stays")
	assert.Equal(t, 1, moves)
}

// MergeAccountsOrders deletes the source account; the copies merged into it
// must follow to the destination so a later hard delete still finds them.
func TestMergeAccounts_MovesTheMergeCopiesToTheDestination(t *testing.T) {
	db, ctx := newTestDB(t)

	source, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-src", "src@test.test")
	destination, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-dst", "dst@test.test")
	seedMergeCopy(t, db, ctx, 900003, source)

	require.NoError(t, db.MergeAccountsOrders(ctx, AccountMergeRequest{
		SourceKeycloakID:      null.StringFrom("kc-src"),
		DestinationKeycloakID: null.StringFrom("kc-dst"),
	}))

	_, _, winnerID := mergeCopies(t, db, ctx, 900003)
	assert.Equal(t, null.IntFrom(destination), winnerID)
}

// Patching an account onto a key that already has one is the same conflict as
// creating a second account for it.
func TestPatchAccount_KeyThatHasAnAccountIsTaken(t *testing.T) {
	db, ctx := newTestDB(t)

	seedAccountWithRows(t, db, ctx, "kc-taken", "taken@test.test")
	mine, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-mine", "mine@test.test")

	err := db.PatchAccount(ctx, Account{UserKey: null.StringFrom("kc-taken")}, mine)
	require.ErrorIs(t, err, common.ErrAccountKeyTaken)
}

// Emails and keys go to Postgres as arguments, so a quote in an email is data,
// and the revert (built from the stored input) puts the account back.
func TestPerformOperation_QuoteInEmail_RenamesAndReverts(t *testing.T) {
	db, ctx := newTestDB(t)
	createSpecialsSep2021(t, db, ctx)

	oldEmail, newEmail := "o'old@test.test", "o'new@test.test"
	accountID, orderID, _, _ := seedAccountWithRows(t, db, ctx, "kc-old", oldEmail)

	str := func(s string) *string { return &s }
	_, err := db.PerformOperation(ctx, OperationReq{
		Type:          str("email_update"),
		OldKeycloakID: str("kc-old"), NewKeycloakID: str("kc-new"),
		OldEmail: str(oldEmail), NewEmail: str(newEmail),
	})
	require.NoError(t, err)

	account := func() (email, key, orderKey string) {
		require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email", "UserKey" FROM accounts WHERE id = $1`, accountID).Scan(&email, &key))
		require.NoError(t, db.pool.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, orderID).Scan(&orderKey))
		return
	}
	email, key, orderKey := account()
	assert.Equal(t, []string{newEmail, "kc-new", "kc-new"}, []string{email, key, orderKey})

	// Two reverts at once: the row lock lets exactly one run.
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- db.RevertOperation(ctx, newEmail, oldEmail) }()
	}
	err1, err2 := <-errs, <-errs
	if err1 != nil {
		err1, err2 = err2, err1
	}
	require.NoError(t, err1)
	require.ErrorIs(t, err2, common.ErrOperationReverted)

	email, key, orderKey = account()
	assert.Equal(t, []string{oldEmail, "kc-old", "kc-old"}, []string{email, key, orderKey})

	require.ErrorIs(t, db.RevertOperation(ctx, "nobody@test.test", oldEmail), common.ErrOperationNotFound)
}

// createSpecialsSep2021 adds the table production has and no migration creates.
func createSpecialsSep2021(t *testing.T, db *OrdersDB, ctx context.Context) {
	t.Helper()
	_, err := db.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS specials_sep2021 (email text)`)
	require.NoError(t, err)
}

// Without an old keycloak id only the email of the new key's account moves,
// in accounts and both specials tables, and the revert moves it back.
func TestPerformOperation_NoOldKey_RenamesEmailAndReverts(t *testing.T) {
	db, ctx := newTestDB(t)
	createSpecialsSep2021(t, db, ctx)

	oldEmail, newEmail := "o'old@test.test", "o'new@test.test"
	accountID, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-1", oldEmail)
	_, err := db.pool.Exec(ctx, `INSERT INTO specials (email, start_date, end_date, category) VALUES ($1, now(), now(), 'c')`, oldEmail)
	require.NoError(t, err)
	_, err = db.pool.Exec(ctx, `INSERT INTO specials_sep2021 (email) VALUES ($1)`, oldEmail)
	require.NoError(t, err)

	str := func(s string) *string { return &s }
	_, err = db.PerformOperation(ctx, OperationReq{
		Type:          str("email_update"),
		NewKeycloakID: str("kc-1"),
		OldEmail:      str(oldEmail), NewEmail: str(newEmail),
	})
	require.NoError(t, err)

	state := func() []string {
		var email, key, special, sep string
		require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email", "UserKey" FROM accounts WHERE id = $1`, accountID).Scan(&email, &key))
		require.NoError(t, db.pool.QueryRow(ctx, `SELECT email FROM specials`).Scan(&special))
		require.NoError(t, db.pool.QueryRow(ctx, `SELECT email FROM specials_sep2021`).Scan(&sep))
		return []string{email, key, special, sep}
	}
	assert.Equal(t, []string{newEmail, "kc-1", newEmail, newEmail}, state())

	require.NoError(t, db.RevertOperation(ctx, newEmail, oldEmail))
	assert.Equal(t, []string{oldEmail, "kc-1", oldEmail, oldEmail}, state())
}

// The revert builds its statements from the stored input; SQL in the
// revert column is a record only and never runs.
func TestRevertOperation_DoesNotRunStoredSQL(t *testing.T) {
	db, ctx := newTestDB(t)
	createSpecialsSep2021(t, db, ctx)

	accountID, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-old", "old@test.test")
	str := func(s string) *string { return &s }
	_, err := db.PerformOperation(ctx, OperationReq{
		Type:          str("email_update"),
		OldKeycloakID: str("kc-old"), NewKeycloakID: str("kc-new"),
		OldEmail: str("old@test.test"), NewEmail: str("new@test.test"),
	})
	require.NoError(t, err)

	_, err = db.pool.Exec(ctx, `UPDATE operation_trace SET revert = '{"queries":[{"sql":"DELETE FROM card_details","args":[]}],"logs":[]}'`)
	require.NoError(t, err)

	require.NoError(t, db.RevertOperation(ctx, "new@test.test", "old@test.test"))

	var email, key string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email", "UserKey" FROM accounts WHERE id = $1`, accountID).Scan(&email, &key))
	assert.Equal(t, []string{"old@test.test", "kc-old"}, []string{email, key})
	var cards int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM card_details`).Scan(&cards))
	assert.Equal(t, 1, cards)
}

// A rename whose old email and key match no account changes nothing and
// writes no trace, instead of reporting success for UPDATE 0.
func TestPerformOperation_NoMatchingAccount_ChangesNothing(t *testing.T) {
	db, ctx := newTestDB(t)
	createSpecialsSep2021(t, db, ctx)
	seedAccountWithRows(t, db, ctx, "kc-1", "old@test.test")

	str := func(s string) *string { return &s }
	_, err := db.PerformOperation(ctx, OperationReq{
		Type:          str("email_update"),
		NewKeycloakID: str("kc-1"),
		OldEmail:      str("typo@test.test"), NewEmail: str("new@test.test"),
	})
	require.ErrorIs(t, err, common.ErrOperationNoAccount)

	var traces int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM operation_trace`).Scan(&traces))
	assert.Zero(t, traces)
}

// If the account's email changed after the rename, the revert would move the
// orders back to the old key but not the account. It is refused instead and
// changes nothing.
func TestRevertOperation_AccountChangedSince_ChangesNothing(t *testing.T) {
	db, ctx := newTestDB(t)
	createSpecialsSep2021(t, db, ctx)

	accountID, orderID, _, _ := seedAccountWithRows(t, db, ctx, "kc-old", "a@test.test")
	str := func(s string) *string { return &s }
	_, err := db.PerformOperation(ctx, OperationReq{
		Type:          str("email_update"),
		OldKeycloakID: str("kc-old"), NewKeycloakID: str("kc-new"),
		OldEmail: str("a@test.test"), NewEmail: str("b@test.test"),
	})
	require.NoError(t, err)
	_, err = db.pool.Exec(ctx, `UPDATE accounts SET "Email" = 'c@test.test' WHERE id = $1`, accountID)
	require.NoError(t, err)

	require.ErrorIs(t, db.RevertOperation(ctx, "b@test.test", "a@test.test"), common.ErrOperationNoAccount)

	var accountKey, orderKey, status string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "UserKey" FROM accounts WHERE id = $1`, accountID).Scan(&accountKey))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, orderID).Scan(&orderKey))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT status FROM operation_trace`).Scan(&status))
	assert.Equal(t, []string{"kc-new", "kc-new", "success"}, []string{accountKey, orderKey, status})
}

// If the old key has an account again (a login since the rename), the
// revert is a key conflict, not a database failure, and changes nothing.
func TestRevertOperation_OldKeyTakenAgain_IsKeyTaken(t *testing.T) {
	db, ctx := newTestDB(t)
	createSpecialsSep2021(t, db, ctx)

	accountID, _, _, _ := seedAccountWithRows(t, db, ctx, "kc-old", "a@test.test")
	str := func(s string) *string { return &s }
	_, err := db.PerformOperation(ctx, OperationReq{
		Type:          str("email_update"),
		OldKeycloakID: str("kc-old"), NewKeycloakID: str("kc-new"),
		OldEmail: str("a@test.test"), NewEmail: str("b@test.test"),
	})
	require.NoError(t, err)
	seedAccountWithRows(t, db, ctx, "kc-old", "again@test.test")

	require.ErrorIs(t, db.RevertOperation(ctx, "b@test.test", "a@test.test"), common.ErrAccountKeyTaken)

	var key, status string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "UserKey" FROM accounts WHERE id = $1`, accountID).Scan(&key))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT status FROM operation_trace`).Scan(&status))
	assert.Equal(t, []string{"kc-new", "success"}, []string{key, status})
}
