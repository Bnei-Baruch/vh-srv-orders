package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strp(s string) *string { return &s }

type identityState struct {
	accountEmail, accountKey, orderKey, specialEmail, specialKey, ordkey string
}

func seedIdentity(t *testing.T, db *OrdersDB, ctx context.Context, email, key string) (accountID, orderID, specialID, paymentID int) {
	t.Helper()
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO accounts ("Email", "UserKey") VALUES ($1, $2) RETURNING id`, email, key).Scan(&accountID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", userkey) VALUES ($1, $2) RETURNING id`, accountID, key).Scan(&orderID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO specials (keycloak_id, email, start_date, end_date, category) VALUES ($1, $2, now(), now(), 'x') RETURNING id`,
		key, email).Scan(&specialID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO payments ("OrderID", "Ordkey") VALUES ($1, 'ord-12345') RETURNING id`, orderID).Scan(&paymentID))
	return
}

func readIdentity(t *testing.T, db *OrdersDB, ctx context.Context, accountID, orderID, specialID, paymentID int) identityState {
	t.Helper()
	var s identityState
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email", "UserKey" FROM accounts WHERE id = $1`, accountID).Scan(&s.accountEmail, &s.accountKey))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, orderID).Scan(&s.orderKey))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT email, keycloak_id FROM specials WHERE id = $1`, specialID).Scan(&s.specialEmail, &s.specialKey))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Ordkey" FROM payments WHERE id = $1`, paymentID).Scan(&s.ordkey))
	return s
}

// A keycloak-id change moves the account, the orders' userkey and the
// specials to the new email and key; the revert puts every one back. Payment
// order keys are not keycloak ids and stay as they are. The emails carry a
// quote: the old code concatenated them into SQL.
func TestPerformOperation_KeycloakChange_AndRevert(t *testing.T) {
	db, ctx := newTestDB(t)

	oldEmail, newEmail := "o'brien@test.test", "obrien@test.test"
	a, o, s, p := seedIdentity(t, db, ctx, oldEmail, "kc-old")
	before := readIdentity(t, db, ctx, a, o, s, p)

	id, err := db.PerformOperation(ctx, OperationReq{
		OldEmail: strp(oldEmail), NewEmail: strp(newEmail),
		OldKeycloakID: strp("kc-old"), NewKeycloakID: strp("kc-new"),
	})
	require.NoError(t, err)
	assert.Equal(t, identityState{
		accountEmail: newEmail, accountKey: "kc-new", orderKey: "kc-new",
		specialEmail: newEmail, specialKey: "kc-new", ordkey: "ord-12345",
	}, readIdentity(t, db, ctx, a, o, s, p))

	var status, output string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT status, output::text FROM operation_trace WHERE id = $1`, id).Scan(&status, &output))
	assert.Equal(t, "success", status)
	assert.JSONEq(t, `{"accounts":1,"orders":1,"specials.email":1,"specials.keycloak_id":1}`, output)

	require.NoError(t, db.RevertOperation(ctx, newEmail, oldEmail))
	assert.Equal(t, before, readIdentity(t, db, ctx, a, o, s, p))

	require.NoError(t, db.pool.QueryRow(ctx, `SELECT status FROM operation_trace WHERE id = $1`, id).Scan(&status))
	assert.Equal(t, "reverted", status)
	assert.Error(t, db.RevertOperation(ctx, newEmail, oldEmail), "a second revert is refused")
}

// Without an old keycloak id only the email changes, on the account whose key
// is the new one. The old code's SQL for this branch was missing a quote and
// could never run.
func TestPerformOperation_EmailOnly_AndRevert(t *testing.T) {
	db, ctx := newTestDB(t)

	a, o, s, p := seedIdentity(t, db, ctx, "before@test.test", "kc-1")
	other, _, _, _ := seedIdentity(t, db, ctx, "before@test.test", "kc-2")

	_, err := db.PerformOperation(ctx, OperationReq{
		OldEmail: strp("before@test.test"), NewEmail: strp("after@test.test"), NewKeycloakID: strp("kc-1"),
	})
	require.NoError(t, err)

	got := readIdentity(t, db, ctx, a, o, s, p)
	assert.Equal(t, "after@test.test", got.accountEmail)
	assert.Equal(t, "kc-1", got.accountKey)
	var otherEmail string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email" FROM accounts WHERE id = $1`, other).Scan(&otherEmail))
	assert.Equal(t, "before@test.test", otherEmail, "another key's account with the same email is untouched")

	require.NoError(t, db.RevertOperation(ctx, "after@test.test", "before@test.test"))
	assert.Equal(t, "before@test.test", readIdentity(t, db, ctx, a, o, s, p).accountEmail)
}

func TestPerformOperation_MissingFieldsAreInvalid(t *testing.T) {
	db, ctx := newTestDB(t)

	_, err := db.PerformOperation(ctx, OperationReq{NewEmail: strp("a@test.test"), NewKeycloakID: strp("kc")})
	assert.Error(t, err, "old_email is required")
}
