package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

// The updates below now bind their row id. These tests cannot tell a bound id
// from one formatted with %d (both reach the same row), so they pin what the
// change must keep, not how it is done: each writes one of two seeded rows by
// id, leaves the other alone, and reports an id naming no row.

type seededPayment struct {
	orderID, paymentID int
}

func seedPaymentRows(t *testing.T, pool *pgxpool.Pool, accountID int, n int) []seededPayment {
	t.Helper()
	ctx := context.Background()
	var orderID int
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "Type", "Status", "Amount", "Currency") VALUES ($1, 'regular', 'new', 180, 'ILS') RETURNING id`,
		accountID).Scan(&orderID))
	seeded := make([]seededPayment, n)
	for i := range seeded {
		seeded[i].orderID = orderID
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO payments ("OrderID", "PaymentStatus", "Amount", "Currency") VALUES ($1, 'pending', 180, 'ILS') RETURNING id`,
			orderID).Scan(&seeded[i].paymentID))
		_, err := pool.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, seeded[i].paymentID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO payments_helphaver (payment_id, status) VALUES ($1, 'pending')`, seeded[i].paymentID)
		require.NoError(t, err)
	}
	return seeded
}

func seedAccount(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var accountID int
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO accounts ("Email", "UserKey") VALUES ($1, $2) RETURNING id`,
		name+"@test.test", "kc-"+name).Scan(&accountID))
	return accountID
}

func newBindTestDB(t *testing.T) (*OrdersDB, *pgxpool.Pool, context.Context) {
	t.Helper()
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, testutil.NewTestPool(t, dbURL), eventstest.WithTestEventBuilder(t, context.Background())
}

func TestPatchCardDetailsById_WritesOnlyThatRow(t *testing.T) {
	db, pool, ctx := newBindTestDB(t)
	accountID := seedAccount(t, pool, "card-patch")
	ids := make([]int, 2)
	for i := range ids {
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO card_details (cc_number, cc_expdate, account_id, gateway_provider, active, token)
			VALUES ($1, '1230', $2, 'pelecard', true, 'tok') RETURNING id`,
			fmt.Sprintf("4580****000%d", i), accountID).Scan(&ids[i]))
	}

	require.NoError(t, db.PatchCardDetailsById(ctx, CardDetails{Active: null.BoolFrom(false)}, ids[0]))

	active := make([]bool, 2)
	for i, id := range ids {
		require.NoError(t, pool.QueryRow(ctx, `SELECT active FROM card_details WHERE id = $1`, id).Scan(&active[i]))
	}
	assert.Equal(t, []bool{false, true}, active)
	assert.ErrorIs(t, db.PatchCardDetailsById(ctx, CardDetails{Active: null.BoolFrom(false)}, ids[1]+1000),
		common.ErrNoRowsAffected)
}

func TestUpdatePelecardPayment_WritesOnlyThatRow(t *testing.T) {
	db, pool, ctx := newBindTestDB(t)
	seeded := seedPaymentRows(t, pool, seedAccount(t, pool, "pelecard-update"), 2)

	require.NoError(t, db.UpdatePelecardPayment(ctx, PaymentUpdate{
		PaymentID: null.IntFrom(seeded[0].paymentID), AuthNo: null.StringFrom("111"),
	}))

	authNos := make([]string, 2)
	for i, s := range seeded {
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COALESCE(auth_no, '') FROM payments_pelecard WHERE payment_id = $1`, s.paymentID).Scan(&authNos[i]))
	}
	assert.Equal(t, []string{"111", ""}, authNos)
	assert.ErrorIs(t, db.UpdatePelecardPayment(ctx, PaymentUpdate{
		PaymentID: null.IntFrom(seeded[1].paymentID + 1000), AuthNo: null.StringFrom("111"),
	}), common.ErrNoRowsAffected)
}

func TestUpdateHelpHavePayment_WritesOnlyThatRow(t *testing.T) {
	db, pool, ctx := newBindTestDB(t)
	seeded := seedPaymentRows(t, pool, seedAccount(t, pool, "helphaver-update"), 2)

	require.NoError(t, db.UpdateHelpHavePayment(ctx, PaymentUpdate{
		PaymentID: null.IntFrom(seeded[0].paymentID), Status: null.StringFrom("approved"),
	}))

	statuses := make([]string, 2)
	for i, s := range seeded {
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT status FROM payments_helphaver WHERE payment_id = $1`, s.paymentID).Scan(&statuses[i]))
	}
	assert.Equal(t, []string{"approved", "pending"}, statuses)
	assert.ErrorIs(t, db.UpdateHelpHavePayment(ctx, PaymentUpdate{
		PaymentID: null.IntFrom(seeded[1].paymentID + 1000), Status: null.StringFrom("approved"),
	}), common.ErrNoRowsAffected)
}

func TestUpdatePayment_WritesOnlyThatPayment(t *testing.T) {
	db, pool, ctx := newBindTestDB(t)
	seeded := seedPaymentRows(t, pool, seedAccount(t, pool, "payment-update"), 2)

	_, err := db.UpdatePayment(ctx, RequestPaid{
		UserKey:  null.StringFrom(fmt.Sprintf("ord-%d", seeded[0].orderID)),
		ParamX:   null.StringFrom(fmt.Sprintf("m-%d", seeded[0].paymentID)),
		Success:  null.StringFrom("1"),
		Token:    null.StringFrom("card-token"),
		AuthNo:   null.StringFrom("0123456"),
		CCNumber: null.StringFrom("458000******1234"),
	})
	require.NoError(t, err)

	type state struct{ status, token, pelecardAuthNo string }
	states := make([]state, 2)
	for i, s := range seeded {
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COALESCE(p."PaymentStatus", ''), COALESCE(p.pelecard_token, ''), COALESCE(pp.auth_no, '')
			FROM payments p JOIN payments_pelecard pp ON pp.payment_id = p.id WHERE p.id = $1`, s.paymentID).
			Scan(&states[i].status, &states[i].token, &states[i].pelecardAuthNo))
	}
	assert.Equal(t, []state{{"success", "card-token", "0123456"}, {"pending", "", ""}}, states)
}

func TestRevertOperation_MarksOnlyThatOperation(t *testing.T) {
	db, pool, ctx := newBindTestDB(t)
	ids := make([]int, 2)
	for i := range ids {
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO operation_trace (input, type, output, status, revert)
			VALUES ($1, 'change_email', '{}', 'done', '{"Queries": [], "Logs": []}') RETURNING id`,
			fmt.Sprintf(`{"new_email": "new%d@test.test", "old_email": "old%d@test.test"}`, i, i)).Scan(&ids[i]))
	}

	require.NoError(t, db.RevertOperation(ctx, "new0@test.test", "old0@test.test"))

	statuses := make([]string, 2)
	for i, id := range ids {
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM operation_trace WHERE id = $1`, id).Scan(&statuses[i]))
	}
	assert.Equal(t, []string{"reverted", "done"}, statuses)
}

// A failed payments_pelecard write reports its own error. It used to wrap the
// payments write's err, nil by then, and print "%!w(<nil>)".
func TestUpdatePayment_ReportsThePelecardWriteError(t *testing.T) {
	db, pool, ctx := newBindTestDB(t)
	seeded := seedPaymentRows(t, pool, seedAccount(t, pool, "payment-pelecard-error"), 1)
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_pelecard_update() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN RAISE EXCEPTION 'pelecard write failed'; END $$;
		CREATE TRIGGER fail_pelecard BEFORE UPDATE ON payments_pelecard
		FOR EACH ROW EXECUTE FUNCTION fail_pelecard_update();`)
	require.NoError(t, err)

	_, err = db.UpdatePayment(ctx, RequestPaid{
		UserKey: null.StringFrom(fmt.Sprintf("ord-%d", seeded[0].orderID)),
		ParamX:  null.StringFrom(fmt.Sprintf("m-%d", seeded[0].paymentID)),
		Success: null.StringFrom("1"),
		Token:   null.StringFrom("card-token"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pelecard write failed")
}
