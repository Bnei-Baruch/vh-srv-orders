package billing

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// staticChargeExecutor returns a fixed response for every Execute call.
type staticChargeExecutor struct {
	response map[string]interface{}
	err      error
}

func (s *staticChargeExecutor) Execute(_ context.Context, _ *pelecard.ChargeRequest, _ pelecard.Terminal, _ uint) (map[string]interface{}, error) {
	return s.response, s.err
}

func successExecutor() pelecard.ChargeExecutor {
	return &staticChargeExecutor{response: map[string]interface{}{"status": "success"}}
}

func declinedExecutor() pelecard.ChargeExecutor {
	return &staticChargeExecutor{response: map[string]interface{}{"status": "declined"}}
}

func gatewayErrorExecutor() pelecard.ChargeExecutor {
	return &staticChargeExecutor{err: fmt.Errorf("connection timeout")}
}

func newIntegrationDB(t *testing.T) (*repo.OrdersDB, context.Context) {
	t.Helper()
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := repo.NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := eventstest.WithTestEventBuilder(t, context.Background())
	return db, ctx
}

// setupOrder creates an account + recurring order + successful payment, returns order ID.
func setupOrder(t *testing.T, db *repo.OrdersDB, ctx context.Context, country string) uint {
	t.Helper()

	accountID, err := db.CreateAccount(ctx, repo.Account{
		Email:     null.StringFrom(fmt.Sprintf("user-%s@example.com", country)),
		FirstName: null.StringFrom("Test"),
		LastName:  null.StringFrom("User"),
		Street:    null.StringFrom("Main St"),
		City:      null.StringFrom("City"),
		Country:   null.StringFrom(country),
		UserKey:   null.StringFrom(fmt.Sprintf("kc-%s", country)),
	})
	require.NoError(t, err)

	var orderID int
	err = db.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "Amount", "Currency", "Type", "Status", "Flag")
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		accountID, 80.0, common.CurrencyNIS, "recurring", common.OrderStatusPaid, common.OrderFlagToRenew,
	).Scan(&orderID)
	require.NoError(t, err)

	var paymentID int
	err = db.QueryRow(ctx,
		`INSERT INTO payments ("Amount", "Currency", "PaymentStatus", "OrderID", "AuthNo", pelecard_token, success)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		80.0, common.CurrencyNIS, common.PaymentStatusSuccess, orderID, "AUTH", "TOKEN", "1",
	).Scan(&paymentID)
	require.NoError(t, err)

	// payments_pelecard record required for finalization
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, paymentID)
	require.NoError(t, err)

	return uint(orderID)
}

// readOrderState reads the current flag and status of an order.
func readOrderState(t *testing.T, db *repo.OrdersDB, ctx context.Context, orderID uint) (flag, status string) {
	t.Helper()
	err := db.QueryRow(ctx,
		`SELECT COALESCE("Flag",''), COALESCE("Status",'') FROM orders WHERE id=$1`, orderID,
	).Scan(&flag, &status)
	require.NoError(t, err)
	return
}

// readPaymentState reads the status and success of the LATEST payment for an order.
func readPaymentState(t *testing.T, db *repo.OrdersDB, ctx context.Context, orderID uint) (status, success, pricingVersion string) {
	t.Helper()
	err := db.QueryRow(ctx,
		`SELECT COALESCE("PaymentStatus",''), COALESCE(success,''), COALESCE(pricing_version,'')
		 FROM payments WHERE "OrderID"=$1 ORDER BY id DESC LIMIT 1`, orderID,
	).Scan(&status, &success, &pricingVersion)
	require.NoError(t, err)
	return
}

// ---------------------------------------------------------------------------
// processOrder — integration tests with real DB
// ---------------------------------------------------------------------------

func TestProcessOrderIntegration_SuccessfulCharge(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	data, err := db.LoadRenewalData(ctx, orderID)
	require.NoError(t, err)

	price := &pricing.ChargePrice{Amount: 20, Currency: common.CurrencyUSD, PricingVersion: "v1"}
	executor := successExecutor()

	payment, err := processOrder(ctx, db, &events.NoopEmitter{}, executor, data, price, pelecard.TokenTerminal)
	require.NoError(t, err)
	assert.Equal(t, "1", payment.Success.String)

	// Verify DB state
	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagRenewed, flag)
	assert.Equal(t, common.OrderStatusPaid, status)

	payStatus, paySuccess, payVersion := readPaymentState(t, db, ctx, orderID)
	assert.Equal(t, common.PaymentStatusSuccess, payStatus)
	assert.Equal(t, "1", paySuccess)
	assert.Equal(t, "v1", payVersion)
}

func TestProcessOrderIntegration_DeclinedCharge(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	data, err := db.LoadRenewalData(ctx, orderID)
	require.NoError(t, err)

	price := &pricing.ChargePrice{Amount: 20, Currency: common.CurrencyUSD, PricingVersion: "v1"}
	executor := declinedExecutor()

	payment, err := processOrder(ctx, db, &events.NoopEmitter{}, executor, data, price, pelecard.TokenTerminal)
	require.NoError(t, err)
	assert.Equal(t, "0", payment.Success.String)

	// Order flag should NOT change (still torenew), status should be nosuccess
	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagToRenew, flag)
	assert.Equal(t, common.OrderStatusNoSuccess, status)

	payStatus, paySuccess, _ := readPaymentState(t, db, ctx, orderID)
	assert.Equal(t, common.PaymentStatusFailed, payStatus)
	assert.Equal(t, "0", paySuccess)
}

func TestProcessOrderIntegration_GatewayError(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	data, err := db.LoadRenewalData(ctx, orderID)
	require.NoError(t, err)

	price := &pricing.ChargePrice{Amount: 20, Currency: common.CurrencyUSD, PricingVersion: "v1"}
	executor := gatewayErrorExecutor()

	payment, err := processOrder(ctx, db, &events.NoopEmitter{}, executor, data, price, pelecard.TokenTerminal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payment gateway error")
	assert.Equal(t, "0", payment.Success.String)

	// Payment should still be written to DB with failed status
	payStatus, paySuccess, _ := readPaymentState(t, db, ctx, orderID)
	assert.Equal(t, common.PaymentStatusFailed, payStatus)
	assert.Equal(t, "0", paySuccess)
}

func TestProcessOrderIntegration_V2PricingEvaluationInDB(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	data, err := db.LoadRenewalData(ctx, orderID)
	require.NoError(t, err)

	price := &pricing.ChargePrice{
		Amount:         35,
		Currency:       common.CurrencyUSD,
		PricingVersion: "v2",
		V2Evaluation: &pricing.V2PricingEvaluation{
			AccountID:   data.Account.ID,
			CountryCode: "US",
			FinalPrice:  pricing.Price{Amount: 35, Currency: common.CurrencyUSD},
		},
	}
	executor := successExecutor()

	_, err = processOrder(ctx, db, &events.NoopEmitter{}, executor, data, price, pelecard.TokenTerminal)
	require.NoError(t, err)

	// Verify pricing evaluation is in the DB
	var evalJSON *string
	err = db.QueryRow(ctx,
		`SELECT pricing_evaluation::text FROM payments WHERE "OrderID"=$1 ORDER BY id DESC LIMIT 1`, orderID,
	).Scan(&evalJSON)
	require.NoError(t, err)
	require.NotNil(t, evalJSON)
	assert.Contains(t, *evalJSON, `"country_code"`)
	assert.Contains(t, *evalJSON, `US`)
	assert.Contains(t, *evalJSON, `"account_id"`)
}

func TestProcessOrderIntegration_V1NoPricingEvaluation(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	data, err := db.LoadRenewalData(ctx, orderID)
	require.NoError(t, err)

	price := &pricing.ChargePrice{Amount: 20, Currency: common.CurrencyUSD, PricingVersion: "v1"}
	executor := successExecutor()

	_, err = processOrder(ctx, db, &events.NoopEmitter{}, executor, data, price, pelecard.TokenTerminal)
	require.NoError(t, err)

	var evalJSON *string
	err = db.QueryRow(ctx,
		`SELECT pricing_evaluation::text FROM payments WHERE "OrderID"=$1 ORDER BY id DESC LIMIT 1`, orderID,
	).Scan(&evalJSON)
	require.NoError(t, err)
	assert.Nil(t, evalJSON, "V1 should have NULL pricing_evaluation")
}

func TestProcessOrderIntegration_ResolvedPriceOverridesOrderAmount(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US") // order has amount=80 NIS

	data, err := db.LoadRenewalData(ctx, orderID)
	require.NoError(t, err)

	// Resolve a different price
	price := &pricing.ChargePrice{Amount: 35, Currency: common.CurrencyUSD, PricingVersion: "v2"}
	executor := successExecutor()

	_, err = processOrder(ctx, db, &events.NoopEmitter{}, executor, data, price, pelecard.TokenTerminal)
	require.NoError(t, err)

	// Verify the payment has the resolved price, not the order's original amount
	var payAmount int
	var payCurrency string
	err = db.QueryRow(ctx,
		`SELECT "Amount", "Currency" FROM payments WHERE "OrderID"=$1 ORDER BY id DESC LIMIT 1`, orderID,
	).Scan(&payAmount, &payCurrency)
	require.NoError(t, err)
	assert.Equal(t, 35, payAmount, "should use resolved price, not order's original 80 NIS")
	assert.Equal(t, common.CurrencyUSD, payCurrency)
}

// ---------------------------------------------------------------------------
// chargeWithPricing — end-to-end integration
// ---------------------------------------------------------------------------

func TestChargeWithPricingIntegration_SingleOrder(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US") // US = v1 pricing, no Priority needed

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	executor := successExecutor()
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, executor)

	count, err := service.chargeWithPricing(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagRenewed, flag)
	assert.Equal(t, common.OrderStatusPaid, status)
}

func TestChargeWithPricingIntegration_TokenDeclined_EMVSucceeds(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)

	// Executor that declines token, succeeds on EMV
	callCount := 0
	executor := &staticChargeExecutor{}
	terminalAwareExecutor := &terminalSwitchExecutor{
		onToken: &staticChargeExecutor{response: map[string]interface{}{"status": "declined"}},
		onEMV:   &staticChargeExecutor{response: map[string]interface{}{"status": "success"}},
	}
	_ = callCount
	_ = executor

	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, terminalAwareExecutor)

	count, err := service.chargeWithPricing(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagRenewed, flag)
	assert.Equal(t, common.OrderStatusPaid, status)
}

// terminalSwitchExecutor returns different results based on terminal.
type terminalSwitchExecutor struct {
	onToken pelecard.ChargeExecutor
	onEMV   pelecard.ChargeExecutor
}

func (e *terminalSwitchExecutor) Execute(ctx context.Context, req *pelecard.ChargeRequest, terminal pelecard.Terminal, orderID uint) (map[string]interface{}, error) {
	if terminal.PMX == pelecard.TokenTerminal.PMX {
		return e.onToken.Execute(ctx, req, terminal, orderID)
	}
	return e.onEMV.Execute(ctx, req, terminal, orderID)
}

func TestChargeWithPricingIntegration_MultipleOrders(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	order1 := setupOrder(t, db, ctx, "US")
	order2 := setupOrder(t, db, ctx, "GB") // GB = v1 (excluded from v2)

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	executor := successExecutor()
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, executor)

	count, err := service.chargeWithPricing(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	flag1, status1 := readOrderState(t, db, ctx, order1)
	assert.Equal(t, common.OrderFlagRenewed, flag1)
	assert.Equal(t, common.OrderStatusPaid, status1)

	flag2, status2 := readOrderState(t, db, ctx, order2)
	assert.Equal(t, common.OrderFlagRenewed, flag2)
	assert.Equal(t, common.OrderStatusPaid, status2)
}

func TestChargeWithPricingIntegration_BothDeclined(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US")

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	executor := declinedExecutor()
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, executor)

	count, err := service.chargeWithPricing(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagToRenew, flag, "should not be flagged renewed on decline")
	assert.Equal(t, common.OrderStatusNoSuccess, status)
}

// ---------------------------------------------------------------------------
// RetryPricingErrors — integration tests with real DB
// ---------------------------------------------------------------------------

// flagOrderAsPricingError sets the pricing_error flag directly in the DB.
func flagOrderAsPricingError(t *testing.T, db *repo.OrdersDB, ctx context.Context, orderID uint) {
	t.Helper()
	_, err := db.Exec(ctx, `UPDATE orders SET "Flag" = $1 WHERE id = $2`, common.OrderFlagPricingError, orderID)
	require.NoError(t, err)
}

func TestRetryPricingErrorsIntegration_NoPricingErrors(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	// Order has torenew flag, not pricing_error
	setupOrder(t, db, ctx, "US")

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, successExecutor())

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRetryPricingErrorsIntegration_SuccessfulRetry(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US") // US = v1 pricing, always resolves

	// Simulate a previous pricing failure by flagging the order
	flagOrderAsPricingError(t, db, ctx, orderID)

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, successExecutor())

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagRenewed, flag)
	assert.Equal(t, common.OrderStatusPaid, status)
}

func TestRetryPricingErrorsIntegration_StillFailsPricing(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "IL") // IL = v2, requires Priority — nil here → always fails

	flagOrderAsPricingError(t, db, ctx, orderID)

	origURL := common.Config.PriorityBaseURL
	common.Config.PriorityBaseURL = ""
	defer func() { common.Config.PriorityBaseURL = origURL }()

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, successExecutor())

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Flag should remain pricing_error (re-set by flagPricingError)
	flag, _ := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagPricingError, flag)
}

func TestRetryPricingErrorsIntegration_DeclinedOrderIsUnflagged(t *testing.T) {
	// Regression: before MarkResolvedForRenew, declined orders kept the pricing_error
	// flag because FinalizeRenewal only touches Flag on charge success.
	// After the fix, pricing_error → torenew happens right after resolution, so a
	// subsequent decline leaves the order with torenew, not pricing_error.
	db, ctx := newIntegrationDB(t)
	orderID := setupOrder(t, db, ctx, "US") // US = v1, resolves deterministically
	flagOrderAsPricingError(t, db, ctx, orderID)

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, declinedExecutor())

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "declined orders don't count as charged")

	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagToRenew, flag, "resolved-but-declined order must not keep pricing_error flag")
	assert.Equal(t, common.OrderStatusNoSuccess, status)
}

func TestRetryPricingErrorsIntegration_MixedOrders(t *testing.T) {
	db, ctx := newIntegrationDB(t)

	// Order 1: pricing_error, US → will resolve and succeed
	order1 := setupOrder(t, db, ctx, "US")
	flagOrderAsPricingError(t, db, ctx, order1)

	// Order 2: torenew (normal flow) → NOT picked up by retry
	order2 := setupOrder(t, db, ctx, "US")

	resolver := pricing.NewPriceResolver(&stubProfileService{}, nil)
	service := NewBillingService(db, nil, &events.NoopEmitter{}, resolver, successExecutor())

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only the pricing_error order should be retried")

	flag1, _ := readOrderState(t, db, ctx, order1)
	assert.Equal(t, common.OrderFlagRenewed, flag1, "retried order should be renewed")

	flag2, _ := readOrderState(t, db, ctx, order2)
	assert.Equal(t, common.OrderFlagToRenew, flag2, "torenew order should not be touched")
}
