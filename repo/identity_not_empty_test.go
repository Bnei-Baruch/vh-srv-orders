package repo

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
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

func TestSchema_RefusesSpecialsEmptyEmail(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO specials (keycloak_id, email, start_date, end_date, category) VALUES ('kc', '', now(), now(), 'x')`)
	requireCheckViolation(t, err, "specials_identifiers_not_empty")
}

func TestSchema_RefusesSpecialsEmptyKeycloakID(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO specials (keycloak_id, email, start_date, end_date, category) VALUES ('', 'a@test.test', now(), now(), 'x')`)
	requireCheckViolation(t, err, "specials_identifiers_not_empty")
}

func TestSchema_RefusesSpecialsWithoutIdentifier(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO specials (start_date, end_date, category) VALUES (now(), now(), 'x')`)
	requireCheckViolation(t, err, "specials_has_identifier")
}

func TestSchema_RefusesAccountsEmptyUserKey(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO accounts ("Email", "UserKey") VALUES ('a@test.test', '')`)
	requireCheckViolation(t, err, "accounts_userkey_not_empty")
}

func TestSchema_RefusesAccountsEmptyEmail(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO accounts ("Email", "UserKey") VALUES ('', 'kc-empty-email')`)
	requireCheckViolation(t, err, "accounts_email_not_empty")
}

func TestSchema_RefusesPaymentsEmptyToken(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO payments (pelecard_token) VALUES ('')`)
	requireCheckViolation(t, err, "payments_pelecard_token_not_empty")
}

func TestSchema_RefusesPaymentsPelecardEmptyToken(t *testing.T) {
	db, ctx := newTestDB(t)
	var paymentID int
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO payments (pelecard_token) VALUES (NULL) RETURNING id`).Scan(&paymentID))
	_, err := db.pool.Exec(ctx, `INSERT INTO payments_pelecard (payment_id, pelecard_token) VALUES ($1, '')`, paymentID)
	requireCheckViolation(t, err, "payments_pelecard_pelecard_token_not_empty")
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

}

type recordingEmitter struct {
	mu     sync.Mutex
	events []events.Event
}

func (r *recordingEmitter) Emit(_ context.Context, es ...events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, es...)
}

func (r *recordingEmitter) Close(context.Context) {}

// vh-srv-profile revokes a special by email only. Ending a keycloak-only special
// would report success while profile keeps the membership, so the revoke is
// refused: the row stays active and no event goes out.
func TestDeleteSpecial_KeycloakOnlyIsRefused(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	rec := new(recordingEmitter)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, rec)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	_, err = db.pool.Exec(ctx,
		`INSERT INTO specials (keycloak_id, start_date, end_date, category)
		 VALUES ('kc-no-email', now() - interval '1 day', now() + interval '1 day', 'hh')`)
	require.NoError(t, err)

	assert.ErrorIs(t, db.DeleteSpecialsByKeycloakId(ctx, "kc-no-email"), common.ErrInvalidValues)

	assert.Empty(t, rec.events)
	var active bool
	require.NoError(t, db.pool.QueryRow(ctx,
		`SELECT end_date > now() FROM specials WHERE keycloak_id = 'kc-no-email'`).Scan(&active))
	assert.True(t, active)
}

// The schema refuses an empty specials.keycloak_id; an empty request value is the
// caller's mistake, not a server error.
func TestSetKeycloakIdByEmail_EmptyValuesAreInvalid(t *testing.T) {
	db, ctx := newTestDB(t)
	insertSpecial(t, db, ctx, "kc-1", "x@test.test", -time.Hour, 24*time.Hour)

	assert.ErrorIs(t, db.SetKeycloakIdByEmail(ctx, "x@test.test", ""), common.ErrInvalidValues)
	assert.ErrorIs(t, db.SetKeycloakIdByEmail(ctx, "", "kc-2"), common.ErrInvalidValues)

	var key string
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT keycloak_id FROM specials WHERE email = 'x@test.test'`).Scan(&key))
	assert.Equal(t, "kc-1", key)
}

// The specials sheet gives a blank email column as "", not NULL. With a
// keycloak id that is still a keycloak-only special, and refused.
func TestCreateSpecial_KeycloakIDWithEmptyEmailIsRefused(t *testing.T) {
	db, ctx := newTestDB(t)

	_, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-blank-email"),
		Email:      null.StringFrom(""),
		StartDate:  null.TimeFrom(time.Now()),
		EndDate:    null.TimeFrom(time.Now().Add(time.Hour)),
		Category:   null.StringFrom("hh"),
	})

	assert.ErrorIs(t, err, common.ErrInvalidValues)
	var n int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM specials WHERE keycloak_id = 'kc-blank-email'`).Scan(&n))
	assert.Zero(t, n)
}

// An email is required; an empty one counts as none. The keycloak-only case
// is TestCreateSpecial_KeycloakOnlyIsRefused.
func TestCreateSpecial_RequiresAnEmail(t *testing.T) {
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
}

// An empty key used to match any account stored with an empty "UserKey", attaching
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

// The back office's "without token" filter used to test for an empty string and so omitted
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
	yes, no := true, false
	all, err := db.GetAllPayments(ctx, 0, 100, nil, &far, "", "", "", "", 0, "", nil, 0, "")
	require.NoError(t, err)
	with, err := db.GetAllPayments(ctx, 0, 100, nil, &far, "", "", "", "", 0, "", &yes, 0, "")
	require.NoError(t, err)
	without, err := db.GetAllPayments(ctx, 0, 100, nil, &far, "", "", "", "", 0, "", &no, 0, "")
	require.NoError(t, err)

	assert.Len(t, all, 3)
	assert.Len(t, with, 1)
	assert.Len(t, without, 2)
}

// accounts_email_not_empty refuses an empty string; the writers store an empty email as
// absent instead of failing on it.
func TestCreateAccount_EmptyEmailIsStoredAsNull(t *testing.T) {
	db, ctx := newTestDB(t)

	id, err := db.CreateAccount(ctx, Account{UserKey: null.StringFrom("kc-1"), Email: null.StringFrom("")})
	require.NoError(t, err)
	require.NoError(t, db.PatchAccount(ctx, Account{Email: null.StringFrom(""), FirstName: null.StringFrom("A")}, id))

	var email null.String
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "Email" FROM accounts WHERE id = $1`, id).Scan(&email))
	assert.False(t, email.Valid)
}

// create_special, like delete_special, sends both identifiers as strings: a
// special with only one of them must not put null on the wire, where
// vh-srv-profile's .(string) read panics.
func TestCreateSpecial_EmitsStringIdentifiers(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	rec := new(recordingEmitter)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, rec)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	_, err = db.CreateSpecial(ctx, Special{
		Email:     null.StringFrom("email-only@test.test"),
		StartDate: null.TimeFrom(time.Now()),
		EndDate:   null.TimeFrom(time.Now().Add(time.Hour)),
		Category:  null.StringFrom("hh"),
	})
	require.NoError(t, err)

	require.Len(t, rec.events, 1)
	assert.Equal(t, "email-only@test.test", rec.events[0].Payload["email"])
	assert.Equal(t, "", rec.events[0].Payload["keycloak_id"])
}

// A keycloak-only special could be granted but never revoked:
// DeleteSpecialById refuses it, since vh-srv-profile revokes by email. So it
// is refused at create, before any row or event exists.
func TestCreateSpecial_KeycloakOnlyIsRefused(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	rec := new(recordingEmitter)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, rec)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	_, err = db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-only"),
		StartDate:  null.TimeFrom(time.Now()),
		EndDate:    null.TimeFrom(time.Now().Add(time.Hour)),
		Category:   null.StringFrom("hh"),
	})

	assert.ErrorIs(t, err, common.ErrInvalidValues)
	assert.Empty(t, rec.events)
	var n int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM specials WHERE keycloak_id = 'kc-only'`).Scan(&n))
	assert.Zero(t, n)
}

// An account stored without an email (an empty one is stored as NULL) can
// still be merged; reading its NULL email used to fail the merge.
func TestMergeAccountsOrders_AccountWithoutEmail(t *testing.T) {
	db, ctx := newTestDB(t)

	src, err := db.CreateAccount(ctx, Account{UserKey: null.StringFrom("kc-src"), Email: null.StringFrom("")})
	require.NoError(t, err)
	dst, err := db.CreateAccount(ctx, Account{UserKey: null.StringFrom("kc-dst"), Email: null.StringFrom("dst@test.test")})
	require.NoError(t, err)
	var orderID int
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO orders ("AccountID") VALUES ($1) RETURNING id`, src).Scan(&orderID))

	require.NoError(t, db.MergeAccountsOrders(ctx, AccountMergeRequest{
		SourceKeycloakID:      null.StringFrom("kc-src"),
		DestinationKeycloakID: null.StringFrom("kc-dst"),
	}))

	var owner int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "AccountID" FROM orders WHERE id = $1`, orderID).Scan(&owner))
	assert.Equal(t, dst, owner)
}

// An empty key names no profile: refused before any profile lookup or
// account creation.
func TestGetOrCreateAccountFromProfile_EmptyKeyIsInvalid(t *testing.T) {
	db, ctx := newTestDB(t)

	_, err := db.GetOrCreateAccountFromProfile(ctx, "")
	assert.ErrorIs(t, err, common.ErrInvalidValues)

	var n int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&n))
	assert.Zero(t, n)
}

// An empty pelecard token is an absent one: no writer puts it in the column,
// where pelecard_token's CHECK would refuse it. A renewal copying a legacy
// empty token reaches these.
func TestPreparePelecardPaymentCreate_SkipsEmptyToken(t *testing.T) {
	cols, _, _ := preparePelecardPaymentCreateQuery(Payment{PelecardToken: null.StringFrom(""), ParamX: null.StringFrom("x")}, 1)
	assert.NotContains(t, cols, "pelecard_token")
}

func TestPreparePelecardPaymentUpdate_SkipsEmptyToken(t *testing.T) {
	set, _ := preparePelecardPaymentUpdateQuery(PaymentUpdate{PelecardToken: null.StringFrom("")})
	assert.NotContains(t, set, "pelecard_token")
}

func TestPreparePelecardPaymentUpdateViaPayment_SkipsEmptyToken(t *testing.T) {
	set, _ := preparePelecardPaymentUpdateViaPaymentStructQuery(Payment{PelecardToken: null.StringFrom("")})
	assert.NotContains(t, set, "pelecard_token")
}

func TestPreparePaymentUpdate_SkipsEmptyToken(t *testing.T) {
	set, _ := preparePaymentUpdateQuery(Payment{PelecardToken: null.StringFrom("")})
	assert.NotContains(t, set, "pelecard_token")
}

// Patching an empty key clears it, as storing an empty string did before.
func TestPatchAccount_EmptyUserKeyClearsIt(t *testing.T) {
	db, ctx := newTestDB(t)

	id, err := db.CreateAccount(ctx, Account{UserKey: null.StringFrom("kc-1"), Email: null.StringFrom("a@test.test")})
	require.NoError(t, err)
	require.NoError(t, db.PatchAccount(ctx, Account{UserKey: null.StringFrom("")}, id))

	var key null.String
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT "UserKey" FROM accounts WHERE id = $1`, id).Scan(&key))
	assert.False(t, key.Valid)
}

// The admin lookup by ParamX reads an account without email or key, which
// migration 28 stores as NULL, the same as before: as empty strings.
func TestFetchPaymentByParamX_AccountWithoutEmailOrKey(t *testing.T) {
	db, ctx := newTestDB(t)

	var accountID, orderID int
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO accounts ("FirstName", "LastName", "Street", "City") VALUES ('F', 'L', 'S', 'C') RETURNING id`).Scan(&accountID))
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "OrderLanguage", "Amount", "Currency", "SKU") VALUES ($1, 'en', 10, 'ILS', 'sku') RETURNING id`,
		accountID).Scan(&orderID))
	_, err := db.pool.Exec(ctx, `INSERT INTO payments ("OrderID", "ParamX") VALUES ($1, 'px-probe')`, orderID)
	require.NoError(t, err)

	p, err := db.FetchPaymentByParamX(ctx, "px-probe")
	require.NoError(t, err)
	assert.Equal(t, "", p.Email)
	assert.Equal(t, "", p.UserKey)
}

// A specials sheet row with an email and a blank keycloak column is created
// with no keycloak_id, not refused by specials_identifiers_not_empty.
func TestCreateSpecial_BlankKeycloakIDIsAbsent(t *testing.T) {
	db, ctx := newTestDB(t)

	id, err := db.CreateSpecial(ctx, Special{
		Email:      null.StringFrom("sheet@test.test"),
		KeycloakId: null.StringFrom(""),
		StartDate:  null.TimeFrom(time.Now()),
		EndDate:    null.TimeFrom(time.Now().Add(time.Hour)),
		Category:   null.StringFrom("hh"),
	})
	require.NoError(t, err)

	var key null.String
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT keycloak_id FROM specials WHERE id = $1`, id).Scan(&key))
	assert.False(t, key.Valid)
}
