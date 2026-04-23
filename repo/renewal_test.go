package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

// setupRenewalTestData creates a complete set of test data for renewal tests:
// account, recurring order (flagged to_renew), and a successful payment.
func setupRenewalTestData(t *testing.T, db *OrdersDB, ctx context.Context) (accountID int, orderID int, paymentID int) {
	t.Helper()

	accountID, err := db.CreateAccount(ctx, Account{
		Email:     null.StringFrom("renewal@example.com"),
		FirstName: null.StringFrom("John"),
		LastName:  null.StringFrom("Doe"),
		Street:    null.StringFrom("Main St"),
		City:      null.StringFrom("TLV"),
		Country:   null.StringFrom("IL"),
		UserKey:   null.StringFrom("kc-renewal-test"),
	})
	require.NoError(t, err)

	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(80),
		Currency:  null.StringFrom(common.CurrencyNIS),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createStr, numStr, args := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createStr, numStr), args...).Scan(&orderID)
	require.NoError(t, err)

	payment := Payment{
		OrderID:       null.IntFrom(orderID),
		Amount:        null.Float64From(80),
		Currency:      null.StringFrom(common.CurrencyNIS),
		PelecardToken: null.StringFrom("TOKEN_ABC"),
		AuthNo:        null.StringFrom("AUTH_123"),
		PaymentStatus: null.StringFrom(common.PaymentStatusSuccess),
		Success:       null.StringFrom("1"),
	}
	createPStr, numPStr, pArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPStr, numPStr), pArgs...).Scan(&paymentID)
	require.NoError(t, err)

	return accountID, orderID, paymentID
}

func newTestDB(t *testing.T) (*OrdersDB, context.Context) {
	t.Helper()
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := eventstest.WithTestEventBuilder(t, context.Background())
	return db, ctx
}

// ---------------------------------------------------------------------------
// LoadRenewalData
// ---------------------------------------------------------------------------

func TestLoadRenewalData_Success(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	assert.Equal(t, orderID, data.Order.ID)
	assert.Equal(t, "renewal@example.com", data.Account.Email.String)
	assert.Equal(t, "TOKEN_ABC", data.PrevPayment.PelecardToken.String)
	assert.Equal(t, "AUTH_123", data.PrevPayment.AuthNo.String)
	assert.Nil(t, data.Card, "no card_details_id on order")
}

func TestLoadRenewalData_WithCardDetails(t *testing.T) {
	db, ctx := newTestDB(t)
	accountID, orderID, _ := setupRenewalTestData(t, db, ctx)

	cardID, err := db.CreateCardDetailsAndGetId(ctx, CardDetails{
		AccountID: null.IntFrom(accountID),
		CCNumber:  null.StringFrom("4111****1111"),
		CCExpDate: null.StringFrom("1225"),
		Active:    null.BoolFrom(true),
		Token:     null.StringFrom("CARD_TOKEN_XYZ"),
	})
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardID, orderID)
	require.NoError(t, err)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	require.NotNil(t, data.Card)
	assert.Equal(t, "CARD_TOKEN_XYZ", data.Card.Token.String)
	assert.Equal(t, "4111****1111", data.Card.CCNumber.String)
}

func TestLoadRenewalData_InactiveCard_ReturnsError(t *testing.T) {
	db, ctx := newTestDB(t)
	accountID, orderID, _ := setupRenewalTestData(t, db, ctx)

	cardID, err := db.CreateCardDetailsAndGetId(ctx, CardDetails{
		AccountID: null.IntFrom(accountID),
		CCNumber:  null.StringFrom("0000****0000"),
		CCExpDate: null.StringFrom("1299"),
		Active:    null.BoolFrom(false),
		Token:     null.StringFrom("INACTIVE_TOKEN"),
	})
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardID, orderID)
	require.NoError(t, err)

	_, err = db.LoadRenewalData(ctx, uint(orderID))
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
	assert.Contains(t, err.Error(), "inactive card")
}

func TestLoadRenewalData_EmptyToken_ReturnsError(t *testing.T) {
	db, ctx := newTestDB(t)
	accountID, orderID, _ := setupRenewalTestData(t, db, ctx)

	cardID, err := db.CreateCardDetailsAndGetId(ctx, CardDetails{
		AccountID: null.IntFrom(accountID),
		CCNumber:  null.StringFrom("0000****0000"),
		CCExpDate: null.StringFrom("1299"),
		Active:    null.BoolFrom(true),
		Token:     null.StringFrom("VALID_TOKEN"),
	})
	require.NoError(t, err)

	// Manually clear the token to simulate empty (DB column is NOT NULL)
	_, err = db.Exec(ctx, `UPDATE card_details SET token = '' WHERE id = $1`, cardID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardID, orderID)
	require.NoError(t, err)

	_, err = db.LoadRenewalData(ctx, uint(orderID))
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
	assert.Contains(t, err.Error(), "empty token")
}

func TestLoadRenewalData_OrderNotFound(t *testing.T) {
	db, ctx := newTestDB(t)

	_, err := db.LoadRenewalData(ctx, 99999)
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
}

func TestLoadRenewalData_NoPayment(t *testing.T) {
	db, ctx := newTestDB(t)

	accountID, err := db.CreateAccount(ctx, Account{Email: null.StringFrom("nopay@example.com")})
	require.NoError(t, err)

	order := Order{AccountID: null.IntFrom(accountID), Status: null.StringFrom(common.OrderStatusPaid)}
	createStr, numStr, args := prepareOrderCreateQuery(order)
	var orderID int
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createStr, numStr), args...).Scan(&orderID)
	require.NoError(t, err)

	_, err = db.LoadRenewalData(ctx, uint(orderID))
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
}

// ---------------------------------------------------------------------------
// CreateRenewalPayment
// ---------------------------------------------------------------------------

func TestCreateRenewalPayment_CreatesPaymentWithResolvedPrice(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 90, common.CurrencyNIS, "v2", null.JSON{}, "t")
	require.NoError(t, err)

	assert.Equal(t, 90.0, payment.Amount.Float64, "should use resolved price, not order amount")
	assert.Equal(t, common.CurrencyNIS, payment.Currency.String)
	assert.Equal(t, common.PaymentStatusPending, payment.PaymentStatus.String)
	assert.Equal(t, orderID, payment.OrderID.Int)
	assert.True(t, payment.ID > 0)
	assert.Contains(t, payment.ParamX.String, "m-")
	assert.Contains(t, payment.Ordkey.String, "ord-")
}

func TestCreateRenewalPayment_CopiesTokenFromPrevPayment(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)

	assert.Equal(t, "TOKEN_ABC", payment.PelecardToken.String)
	assert.Equal(t, "AUTH_123", payment.AuthNo.String)
}

func TestCreateRenewalPayment_CardOverridesPrevPaymentToken(t *testing.T) {
	db, ctx := newTestDB(t)
	accountID, orderID, _ := setupRenewalTestData(t, db, ctx)

	cardID, err := db.CreateCardDetailsAndGetId(ctx, CardDetails{
		AccountID: null.IntFrom(accountID),
		Active:    null.BoolFrom(true),
		Token:     null.StringFrom("CARD_TOKEN_NEW"),
		CCNumber:  null.StringFrom("5555****4444"),
		CCExpDate: null.StringFrom("0127"),
	})
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardID, orderID)
	require.NoError(t, err)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "e")
	require.NoError(t, err)

	assert.Equal(t, "CARD_TOKEN_NEW", payment.PelecardToken.String)
	assert.Equal(t, "5555****4444", payment.CCNumber.String)
	assert.Equal(t, "0127", payment.CCExpDate.String)
}

func TestCreateRenewalPayment_PricingVersionStoredInDB(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	evalJSON := null.JSONFrom([]byte(`{"account_id":10,"country_code":"IL"}`))
	payment, err := db.CreateRenewalPayment(ctx, data, 20, common.CurrencyUSD, "v2", evalJSON, "t")
	require.NoError(t, err)
	assert.Equal(t, "v2", payment.PricingVersion.String)

	// Verify pricing fields are actually in the DB, not just on the struct
	var dbVersion, dbEval *string
	err = db.QueryRow(ctx,
		`SELECT pricing_version, pricing_evaluation::text FROM payments WHERE id = $1`, payment.ID).
		Scan(&dbVersion, &dbEval)
	require.NoError(t, err)
	require.NotNil(t, dbVersion)
	assert.Equal(t, "v2", *dbVersion)
	require.NotNil(t, dbEval)
	assert.Contains(t, *dbEval, "IL")
}

func TestCreateRenewalPayment_PelecardRecordCreated(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)

	// Verify pelecard record exists
	var pelecardCount int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM payments_pelecard WHERE payment_id = $1`, payment.ID).Scan(&pelecardCount)
	require.NoError(t, err)
	assert.Equal(t, 1, pelecardCount)
}

// ---------------------------------------------------------------------------
// FinalizeRenewal
// ---------------------------------------------------------------------------

func TestFinalizeRenewal_SuccessfulPayment(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)

	// Simulate successful gateway response
	payment.PaymentStatus = null.StringFrom(common.PaymentStatusSuccess)
	payment.Success = null.StringFrom("1")
	payment.Terminal = null.StringFrom("token")

	err = db.FinalizeRenewal(ctx, uint(orderID), payment)
	require.NoError(t, err)

	// Verify order was flagged as renewed and status updated
	order, err := db.GetOrderByID(ctx, uint(orderID))
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagRenewed, order.Flag.String)
	assert.Equal(t, common.OrderStatusPaid, order.Status.String)
	assert.True(t, order.PaymentDate.Valid)
}

func TestFinalizeRenewal_DeclinedPayment(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)

	// Simulate declined response
	payment.PaymentStatus = null.StringFrom(common.PaymentStatusFailed)
	payment.Success = null.StringFrom("0")

	err = db.FinalizeRenewal(ctx, uint(orderID), payment)
	require.NoError(t, err)

	// Order should NOT be flagged as renewed, status should be nosuccess
	order, err := db.GetOrderByID(ctx, uint(orderID))
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, order.Flag.String, "flag should not change on decline")
	assert.Equal(t, common.OrderStatusNoSuccess, order.Status.String)
}

func TestFinalizeRenewal_PaymentRecordUpdated(t *testing.T) {
	db, ctx := newTestDB(t)
	_, orderID, _ := setupRenewalTestData(t, db, ctx)

	data, err := db.LoadRenewalData(ctx, uint(orderID))
	require.NoError(t, err)

	payment, err := db.CreateRenewalPayment(ctx, data, 80, common.CurrencyNIS, "v1", null.JSON{}, "t")
	require.NoError(t, err)

	payment.PaymentStatus = null.StringFrom(common.PaymentStatusSuccess)
	payment.Success = null.StringFrom("1")
	payment.Terminal = null.StringFrom("emv")

	err = db.FinalizeRenewal(ctx, uint(orderID), payment)
	require.NoError(t, err)

	// Read back the payment and verify it was updated
	var updatedStatus, updatedSuccess string
	err = db.QueryRow(ctx, `SELECT "PaymentStatus", success FROM payments WHERE id = $1`, payment.ID).
		Scan(&updatedStatus, &updatedSuccess)
	require.NoError(t, err)
	assert.Equal(t, common.PaymentStatusSuccess, updatedStatus)
	assert.Equal(t, "1", updatedSuccess)
}
