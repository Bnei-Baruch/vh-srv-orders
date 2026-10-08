package repo

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Migration 28 makes NULL the only spelling of "absent" for the identity
// columns below. These tests pin both halves: the schema refuses '', and the
// code reads NULL as absent instead of crashing on it or matching it.

func requireCheckViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "want check_violation")
	assert.Equal(t, constraint, pgErr.ConstraintName)
}

func TestSchema_RefusesEmptyIdentifiers(t *testing.T) {
	db, ctx := newTestDB(t)

	cases := []struct {
		name, sql, constraint string
	}{
		{"specials empty email", `INSERT INTO specials (keycloak_id, email, start_date, end_date, category) VALUES ('kc', '', now(), now(), 'x')`, "specials_identifiers_not_empty"},
		{"specials empty keycloak_id", `INSERT INTO specials (keycloak_id, email, start_date, end_date, category) VALUES ('', 'a@test.test', now(), now(), 'x')`, "specials_identifiers_not_empty"},
		{"specials no identifier", `INSERT INTO specials (start_date, end_date, category) VALUES (now(), now(), 'x')`, "specials_has_identifier"},
		{"accounts empty UserKey", `INSERT INTO accounts ("Email", "UserKey") VALUES ('a@test.test', '')`, "accounts_userkey_not_empty"},
		{"payments empty token", `INSERT INTO payments (pelecard_token) VALUES ('')`, "payments_pelecard_token_not_empty"},
		{"payments_pelecard empty token", `INSERT INTO payments_pelecard (payment_id, pelecard_token) VALUES ((SELECT id FROM payments LIMIT 1), '')`, "payments_pelecard_pelecard_token_not_empty"},
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO payments (pelecard_token) VALUES (NULL)`)
	require.NoError(t, err)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.pool.Exec(ctx, tc.sql)
			requireCheckViolation(t, err, tc.constraint)
		})
	}
}

// One specials row without an email used to make GetUniqueEmailsFromSpecial fail,
// which specialActivator hands to LogFatal: no specials activated for anyone.
func TestSpecials_NullEmailIsAbsentNotFatal(t *testing.T) {
	db, ctx := newTestDB(t)

	_, err := db.pool.Exec(ctx,
		`INSERT INTO specials (keycloak_id, start_date, end_date, category)
		 VALUES ('kc-no-email', now() - interval '1 day', now() + interval '1 day', 'hh')`)
	require.NoError(t, err)
	insertSpecial(t, db, ctx, "kc-with-email", "with@test.test", -time.Hour, 24*time.Hour)

	emails, err := db.GetUniqueEmailsFromSpecial(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"with@test.test"}, emails)

	require.NoError(t, db.DeleteSpecialsByKeycloakId(ctx, "kc-no-email"),
		"revoking a keycloak-only special must not die scanning its NULL email")
}

func TestCreateSpecial_RequiresAnIdentifier(t *testing.T) {
	db, ctx := newTestDB(t)

	base := Special{
		StartDate: null.TimeFrom(time.Now().Add(-time.Hour)),
		EndDate:   null.TimeFrom(time.Now().Add(time.Hour)),
		Category:  null.StringFrom("hh"),
	}

	_, err := db.CreateSpecial(ctx, base)
	assert.ErrorIs(t, err, common.ErrInvalidValues, "no identifier")

	empty := base
	empty.KeycloakId = null.StringFrom("")
	empty.Email = null.StringFrom("")
	_, err = db.CreateSpecial(ctx, empty)
	assert.ErrorIs(t, err, common.ErrInvalidValues, "empty identifiers are no identifiers")

	keycloakOnly := base
	keycloakOnly.KeycloakId = null.StringFrom("kc-only")
	keycloakOnly.Email = null.StringFrom("")
	id, err := db.CreateSpecial(ctx, keycloakOnly)
	require.NoError(t, err)
	var email null.String
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT email FROM specials WHERE id = $1`, id).Scan(&email))
	assert.False(t, email.Valid, "an empty email is stored as NULL")
}

// An empty key used to match any account stored with "UserKey" = ”, attaching
// one person's orders to a stranger's account.
func TestGetOrCreateAccount_EmptyKeyNeverMatches(t *testing.T) {
	db, ctx := newTestDB(t)

	first, err := db.GetOrCreateAccount(ctx, Account{Email: null.StringFrom("first@test.test"), UserKey: null.StringFrom("")})
	require.NoError(t, err)
	second, err := db.GetOrCreateAccount(ctx, Account{Email: null.StringFrom("second@test.test"), UserKey: null.StringFrom("")})
	require.NoError(t, err)
	third, err := db.GetOrCreateAccount(ctx, Account{Email: null.StringFrom("third@test.test")})
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.NotEqual(t, first, third)
	assert.NotEqual(t, second, third)

	var withEmptyKey int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE "UserKey" IS NOT NULL`).Scan(&withEmptyKey))
	assert.Zero(t, withEmptyKey, "an empty key is stored as NULL")

	keyed, err := db.GetOrCreateAccount(ctx, Account{Email: null.StringFrom("k@test.test"), UserKey: null.StringFrom("kc-1")})
	require.NoError(t, err)
	again, err := db.GetOrCreateAccount(ctx, Account{Email: null.StringFrom("k@test.test"), UserKey: null.StringFrom("kc-1")})
	require.NoError(t, err)
	assert.Equal(t, keyed, again, "a real key still finds its account")
}

// An account created without a key has a NULL "UserKey"; deleting its data used
// to fail scanning that NULL into a string.
func TestHardDeleteAllUserData_KeylessAccount(t *testing.T) {
	db, ctx := newTestDB(t)

	id, err := db.CreateAccount(ctx, Account{Email: null.StringFrom("nokey@test.test")})
	require.NoError(t, err)

	require.NoError(t, db.HardDeleteAllUserDataByAccountID(ctx, id, ""))
}

// The back office's "without token" filter used to test = ” and so omitted
// exactly the payments created without a token.
func TestGetAllPayments_TokenFilterCountsNullAsWithout(t *testing.T) {
	db, ctx := newTestDB(t)

	for _, p := range []Payment{
		{PelecardToken: null.StringFrom("tok"), PaymentStatus: null.StringFrom("success")},
		{PaymentStatus: null.StringFrom("success")},
		{PelecardToken: null.StringFrom(""), PaymentStatus: null.StringFrom("success")},
	} {
		cols, nums, args := preparePaymentCreateQuery(p)
		_, err := db.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s)`, cols, nums), args...)
		require.NoError(t, err)
	}

	far := time.Now().Add(time.Hour)
	all, err := db.GetAllPayments(ctx, 0, 100, "", &far, "", "", "", "", 0, "", 0, "")
	require.NoError(t, err)
	with, err := db.GetAllPayments(ctx, 0, 100, "", &far, "", "", "", "", 0, "true", 0, "")
	require.NoError(t, err)
	without, err := db.GetAllPayments(ctx, 0, 100, "", &far, "", "", "", "", 0, "false", 0, "")
	require.NoError(t, err)

	assert.Len(t, all, 3)
	assert.Len(t, with, 1)
	assert.Len(t, without, 2)
}
