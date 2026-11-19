package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

func seedBuilderAccount(t *testing.T, db *OrdersDB, ctx context.Context, email string) (id int) {
	t.Helper()
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO accounts ("Email") VALUES ($1) RETURNING id`, email).Scan(&id))
	return id
}

func seedBuilderOrder(t *testing.T, db *OrdersDB, ctx context.Context, email string) (id int) {
	t.Helper()
	require.NoError(t, db.pool.QueryRow(ctx, `INSERT INTO orders ("AccountID") VALUES ($1) RETURNING id`,
		seedBuilderAccount(t, db, ctx, email)).Scan(&id))
	return id
}

func TestColVals_RendersInsertAndSetLists(t *testing.T) {
	var c colVals
	c.add(`"Amount"`, 10.5)
	c.add("status", "paid")

	cols, nums, args := c.insertLists()
	assert.Equal(t, `"Amount",status`, cols)
	assert.Equal(t, "$1,$2", nums)
	assert.Equal(t, queryArgs{10.5, "paid"}, args)

	set, args := c.setList()
	assert.Equal(t, `"Amount"=$1,status=$2`, set)
	assert.Equal(t, "$3", args.arg(7), "a WHERE value is bound after the SET values")
	assert.Equal(t, queryArgs{10.5, "paid", 7}, args)
}

func TestColVals_EmptyRendersNothing(t *testing.T) {
	var c colVals
	assert.True(t, c.empty())
	cols, nums, args := c.insertLists()
	assert.Empty(t, cols)
	assert.Empty(t, nums)
	assert.Empty(t, args)
}

func TestPatchCardDetailsById_WritesSetFieldsOnly(t *testing.T) {
	db, ctx := newTestDB(t)
	accountID := seedBuilderAccount(t, db, ctx, "card@test.test")
	id, err := db.CreateCardDetailsAndGetId(ctx, CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("4580"),
		CCExpDate:       null.StringFrom("1230"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("tok-1"),
	})
	require.NoError(t, err)

	require.NoError(t, db.PatchCardDetailsById(ctx, CardDetails{Active: null.BoolFrom(false), Token: null.StringFrom("tok-2")}, id))

	card, err := db.GetCardDetailById(ctx, id)
	require.NoError(t, err)
	assert.False(t, card.Active.Bool)
	assert.Equal(t, "tok-2", card.Token.String)
	assert.Equal(t, "4580", card.CCNumber.String, "an unset field is left as it was")
}

func TestPatchCardDetailsById_NothingToUpdate(t *testing.T) {
	db, ctx := newTestDB(t)
	assert.ErrorIs(t, db.PatchCardDetailsById(ctx, CardDetails{}, 1), common.ErrInvalidValues)
}

func TestPatchCardDetailsById_MissingRow(t *testing.T) {
	db, ctx := newTestDB(t)
	assert.ErrorIs(t, db.PatchCardDetailsById(ctx, CardDetails{Active: null.BoolFrom(false)}, 999999), common.ErrNoRowsAffected)
}

func TestUpdatePelecardPayment_WritesSetFieldsOnly(t *testing.T) {
	db, ctx := newTestDB(t)
	orderID := seedBuilderOrder(t, db, ctx, "pele@test.test")
	p, err := db.CreatePayment(ctx, RequestOrder{Amount: null.Float64From(80)}, orderID)
	require.NoError(t, err)

	require.NoError(t, db.UpdatePelecardPayment(ctx, PaymentUpdate{
		PaymentID: null.IntFrom(p.ID),
		AuthNo:    null.StringFrom("AUTH-1"),
		CCBrand:   null.StringFrom("visa"),
	}))

	var authNo, brand null.String
	var amount null.Float64
	require.NoError(t, db.pool.QueryRow(ctx,
		`SELECT auth_no, cc_brand, amount FROM payments_pelecard WHERE payment_id=$1`, p.ID).Scan(&authNo, &brand, &amount))
	assert.Equal(t, "AUTH-1", authNo.String)
	assert.Equal(t, "visa", brand.String)
	assert.Equal(t, 80.0, amount.Float64, "an unset field is left as it was")
}

func TestUpdatePelecardPayment_NothingToUpdate(t *testing.T) {
	db, ctx := newTestDB(t)
	assert.ErrorIs(t, db.UpdatePelecardPayment(ctx, PaymentUpdate{PaymentID: null.IntFrom(1)}), common.ErrInvalidValues)
}

func TestUpdatePelecardPayment_MissingRow(t *testing.T) {
	db, ctx := newTestDB(t)
	err := db.UpdatePelecardPayment(ctx, PaymentUpdate{PaymentID: null.IntFrom(999999), AuthNo: null.StringFrom("x")})
	assert.ErrorIs(t, err, common.ErrNoRowsAffected)
}

func TestUpdateHelpHavePayment_WritesSetFieldsOnly(t *testing.T) {
	db, ctx := newTestDB(t)
	orderID := seedBuilderOrder(t, db, ctx, "hh@test.test")
	p, err := db.CreatePayment(ctx, RequestOrder{PaymentType: null.StringFrom(common.PaymentTypeHelpHaver)}, orderID)
	require.NoError(t, err)

	var status null.String
	require.NoError(t, db.pool.QueryRow(ctx,
		`SELECT status FROM payments_helphaver WHERE payment_id=$1`, p.ID).Scan(&status))
	assert.Equal(t, common.PaymentStatusPending, status.String, "the create wrote the payment's status")

	require.NoError(t, db.UpdateHelpHavePayment(ctx, PaymentUpdate{
		PaymentID:        null.IntFrom(p.ID),
		RejectionMessage: null.StringFrom("no proof"),
	}))

	var validation, rejection null.String
	require.NoError(t, db.pool.QueryRow(ctx,
		`SELECT status, validation_message, rejection_message FROM payments_helphaver WHERE payment_id=$1`, p.ID).
		Scan(&status, &validation, &rejection))
	assert.Equal(t, common.PaymentStatusPending, status.String, "an unset field is left as it was")
	assert.False(t, validation.Valid)
	assert.Equal(t, "no proof", rejection.String)
}

func TestUpdateHelpHavePayment_NothingToUpdate(t *testing.T) {
	db, ctx := newTestDB(t)
	assert.ErrorIs(t, db.UpdateHelpHavePayment(ctx, PaymentUpdate{PaymentID: null.IntFrom(1)}), common.ErrInvalidValues)
}

func TestUpdateHelpHavePayment_MissingRow(t *testing.T) {
	db, ctx := newTestDB(t)
	err := db.UpdateHelpHavePayment(ctx, PaymentUpdate{PaymentID: null.IntFrom(999999), Status: null.StringFrom("x")})
	assert.ErrorIs(t, err, common.ErrNoRowsAffected)
}
