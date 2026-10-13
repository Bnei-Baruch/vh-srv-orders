package repo

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The list filters used to paste request values into SQL inside quotes. A value
// with a quote then either broke the query or rewrote it; these pin that every
// filter value is now compared as data.

const (
	quotedEmail = "o'brien@test.test"
	// Closes the quoted literal and the LOWER( call, then ORs in a true condition.
	injection = "x') OR ('1'='1"
	// The same for the one filter that is not wrapped in LOWER(.
	bareInjection = "x' OR '1'='1"
)

func seedFilterRows(t *testing.T, db *OrdersDB, ctx context.Context) {
	t.Helper()
	for _, row := range []struct{ email, key string }{{quotedEmail, "kc-quoted"}, {"other@test.test", "kc-other"}} {
		var accountID, orderID, paymentID int
		require.NoError(t, db.pool.QueryRow(ctx,
			`INSERT INTO accounts ("Email", "UserKey") VALUES ($1, $2) RETURNING id`, row.email, row.key).Scan(&accountID))
		require.NoError(t, db.pool.QueryRow(ctx,
			`INSERT INTO orders ("AccountID", userkey, "Currency", "Status", "ProductType", "Organization", "Type")
			 VALUES ($1, $2, 'ILS', 'paid', 'hh', 'org', 'single') RETURNING id`, accountID, row.key).Scan(&orderID))
		require.NoError(t, db.pool.QueryRow(ctx,
			`INSERT INTO payments ("OrderID", "PaymentType", "PaymentStatus") VALUES ($1, 'cc', 'ok') RETURNING id`,
			orderID).Scan(&paymentID))
		_, err := db.pool.Exec(ctx,
			`INSERT INTO payments_offline (payment_id, payment_method) VALUES ($1, 'cash')`, paymentID)
		require.NoError(t, err)
	}
}

func TestListFilters_ValuesAreParameters(t *testing.T) {
	db, ctx := newTestDB(t)
	seedFilterRows(t, db, ctx)
	future := time.Now().Add(time.Hour)

	t.Run("GetAccount", func(t *testing.T) {
		acc, err := db.GetAccount(ctx, 0, quotedEmail)
		require.NoError(t, err)
		assert.Equal(t, quotedEmail, acc.Email.String)
		_, err = db.GetAccount(ctx, 0, injection)
		assert.ErrorIs(t, err, pgx.ErrNoRows, "matches no account")
	})

	t.Run("GetAllAccounts", func(t *testing.T) {
		accs, err := db.GetAllAccounts(ctx, 0, 10, quotedEmail)
		require.NoError(t, err)
		assert.Len(t, accs, 1)
		accs, err = db.GetAllAccounts(ctx, 0, 10, injection)
		require.NoError(t, err)
		assert.Empty(t, accs)
	})

	t.Run("GetAllOrders", func(t *testing.T) {
		get := func(productType, currency, status, org, email, key string) []Order {
			t.Helper()
			orders, err := db.GetAllOrders(ctx, 0, 10, "", &future, productType, currency, status, org, email, 0, key, "", "")
			require.NoError(t, err)
			return *orders
		}
		assert.Len(t, get("", "", "", "", quotedEmail, ""), 1)
		assert.Len(t, get("", "", "", "", "", "kc-quoted"), 1)
		for name, orders := range map[string][]Order{
			"product type": get(injection, "", "", "", "", ""),
			"currency":     get("", injection, "", "", "", ""),
			"status":       get("", "", injection, "", "", ""),
			"organization": get("", "", "", injection, "", ""),
			"email":        get("", "", "", "", injection, ""),
			"keycloak id":  get("", "", "", "", "", bareInjection),
		} {
			assert.Empty(t, orders, name)
		}
	})

	t.Run("GetAllPayments", func(t *testing.T) {
		get := func(paymentType, paymentStatus, orderType, email string) []Payment {
			t.Helper()
			payments, err := db.GetAllPayments(ctx, 0, 10, "", &time.Time{}, paymentType, paymentStatus, orderType, email, 0, "", 0, "")
			require.NoError(t, err)
			return payments
		}
		assert.Len(t, get("", "", "", quotedEmail), 1)
		for name, payments := range map[string][]Payment{
			"payment type":   get(injection, "", "", ""),
			"payment status": get("", injection, "", ""),
			"order type":     get("", "", injection, ""),
			"email":          get("", "", "", injection),
		} {
			assert.Empty(t, payments, name)
		}
	})

	t.Run("GetPaymentActivities", func(t *testing.T) {
		acts, err := db.GetPaymentActivities(ctx, "", "o'brien", "", "", 0, 10)
		require.NoError(t, err)
		assert.Len(t, acts, 1, "email is a substring search")
		n, err := db.GetTotalParticipationStatusCount(ctx, "", "o'brien", "", "")
		require.NoError(t, err)
		assert.Equal(t, 1, n)

		for _, args := range [][3]string{{"x%') OR ('1'='1", "", ""}, {"", injection, ""}, {"", "", injection}} {
			acts, err := db.GetPaymentActivities(ctx, "", args[0], args[1], args[2], 0, 10)
			require.NoError(t, err)
			assert.Empty(t, acts, args)
			n, err := db.GetTotalParticipationStatusCount(ctx, "", args[0], args[1], args[2])
			require.NoError(t, err)
			assert.Zero(t, n, args)
		}
	})

	// userKey is what pins a non-admin. Whatever email comes with it, only
	// that keycloak id's accounts can match.
	t.Run("GetPaymentActivities userKey", func(t *testing.T) {
		acts, err := db.GetPaymentActivities(ctx, "kc-quoted", "", "", "", 0, 10)
		require.NoError(t, err)
		require.Len(t, acts, 1, "no email: just the pinned user's accounts")
		assert.Equal(t, quotedEmail, acts[0].Email.String)
		n, err := db.GetTotalParticipationStatusCount(ctx, "kc-quoted", "", "", "")
		require.NoError(t, err)
		assert.Equal(t, 1, n)

		acts, err = db.GetPaymentActivities(ctx, "kc-other", quotedEmail, "", "", 0, 10)
		require.NoError(t, err)
		assert.Empty(t, acts, "another user's email does not reach past the pin")
		n, err = db.GetTotalParticipationStatusCount(ctx, "kc-other", quotedEmail, "", "")
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("GetOfflinePayments", func(t *testing.T) {
		payments, err := db.GetOfflinePayments(ctx, 0, 10, "cash", "")
		require.NoError(t, err)
		assert.Len(t, payments, 2)
		payments, err = db.GetOfflinePayments(ctx, 0, 10, bareInjection, "")
		require.NoError(t, err)
		assert.Empty(t, payments)
	})
}
