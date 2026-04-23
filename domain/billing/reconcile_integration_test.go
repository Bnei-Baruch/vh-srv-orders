package billing

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// setupUnfinalizedOrder creates an order + a pending payment that simulates a CHARGE_SUCCESS_DB_FAIL:
// gateway charged the customer, but FinalizeRenewal failed leaving the payment in "pending" state.
// Returns (orderID, paymentID).
func setupUnfinalizedOrder(t *testing.T, db *repo.OrdersDB, ctx context.Context, country string) (uint, int) {
	t.Helper()
	return setupUnfinalizedOrderWithPayment(t, db, ctx, country, 90.0, common.CurrencyNIS, "v2")
}

// setupUnfinalizedOrderWithPayment is a parametrized variant that lets tests vary the
// payment's amount, currency, and pricing_version so consistency checks can be exercised.
func setupUnfinalizedOrderWithPayment(t *testing.T, db *repo.OrdersDB, ctx context.Context, country string, amount float64, currency, pricingVersion string) (uint, int) {
	t.Helper()

	accountID, err := db.CreateAccount(ctx, repo.Account{
		Email:     null.StringFrom(fmt.Sprintf("reconcile-%s-%s@example.com", country, currency)),
		FirstName: null.StringFrom("Test"),
		LastName:  null.StringFrom("User"),
		Street:    null.StringFrom("Main St"),
		City:      null.StringFrom("City"),
		Country:   null.StringFrom(country),
		UserKey:   null.StringFrom(fmt.Sprintf("kc-r-%s-%s", country, currency)),
	})
	require.NoError(t, err)

	var orderID int
	err = db.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "Amount", "Currency", "Type", "Status", "Flag")
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		accountID, amount, currency, "recurring", common.OrderStatusPaid, common.OrderFlagToRenew,
	).Scan(&orderID)
	require.NoError(t, err)

	// Create the "pending" payment that was left behind when FinalizeRenewal failed
	var paymentID int
	err = db.QueryRow(ctx,
		`INSERT INTO payments ("Amount", "Currency", "PaymentStatus", "OrderID", "AuthNo", pelecard_token, success, pricing_version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		amount, currency, common.PaymentStatusPending, orderID, "AUTH-R", "TOKEN-R", "", pricingVersion,
	).Scan(&paymentID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, paymentID)
	require.NoError(t, err)

	return uint(orderID), paymentID
}

// ---------------------------------------------------------------------------
// Reconcile — integration tests with real DB
// ---------------------------------------------------------------------------

func TestReconcileIntegration_SuccessfulReconciliation(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL")

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID,
		AccountID:      10,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		Terminal:       pelecard.TokenTerminal.Name,
	}})

	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Reconciled)
	assert.Equal(t, 0, result.AlreadyReconciled)
	assert.Equal(t, 0, result.Failed)

	// Verify DB state
	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagRenewed, flag)
	assert.Equal(t, common.OrderStatusPaid, status)

	var payStatus, paySuccess string
	err := db.QueryRow(ctx,
		`SELECT COALESCE("PaymentStatus",''), COALESCE(success,'') FROM payments WHERE id=$1`, paymentID,
	).Scan(&payStatus, &paySuccess)
	require.NoError(t, err)
	assert.Equal(t, common.PaymentStatusSuccess, payStatus)
	assert.Equal(t, "1", paySuccess)
}

func TestReconcileIntegration_AlreadyReconciled(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL")

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	entry := ReconcileEntry{
		OrderID:        orderID,
		AccountID:      10,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		Terminal:       pelecard.TokenTerminal.Name,
	}

	// First reconcile
	result := service.Reconcile(ctx, []ReconcileEntry{entry})
	assert.Equal(t, 1, result.Reconciled)

	// Second reconcile — should detect already reconciled
	result = service.Reconcile(ctx, []ReconcileEntry{entry})
	assert.Equal(t, 0, result.Reconciled)
	assert.Equal(t, 1, result.AlreadyReconciled)
}

func TestReconcileIntegration_PaymentNotFound(t *testing.T) {
	db, ctx := newIntegrationDB(t)

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:   999,
		AccountID: 999,
		PaymentID: 999999,
		Amount:    10,
		Currency:  common.CurrencyUSD,
		Terminal:  "token",
	}})

	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 0, result.Reconciled)
	assert.Equal(t, 1, result.Failed)
}

func TestReconcileIntegration_MultipleEntries(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	order1, payment1 := setupUnfinalizedOrderWithPayment(t, db, ctx, "IL", 90, common.CurrencyNIS, "v2")
	order2, payment2 := setupUnfinalizedOrderWithPayment(t, db, ctx, "US", 20, common.CurrencyUSD, "v1")

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{
		{OrderID: order1, PaymentID: payment1, Amount: 90, Currency: common.CurrencyNIS, PricingVersion: "v2", Terminal: "token"},
		{OrderID: order2, PaymentID: payment2, Amount: 20, Currency: common.CurrencyUSD, PricingVersion: "v1", Terminal: "emv"},
	})

	assert.Equal(t, 2, result.Total)
	assert.Equal(t, 2, result.Reconciled)

	flag1, _ := readOrderState(t, db, ctx, order1)
	flag2, _ := readOrderState(t, db, ctx, order2)
	assert.Equal(t, common.OrderFlagRenewed, flag1)
	assert.Equal(t, common.OrderFlagRenewed, flag2)
}

func TestReconcileIntegration_OrderIDMismatch_Failed(t *testing.T) {
	// Guard: FinalizeRenewal uses both the orderID parameter and payment.OrderID
	// internally. If they diverge, one order gets Flag=renewed while another gets
	// Status=paid — silent data corruption. Reconcile must refuse the entry.
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL")

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	// Entry claims a different order_id than what's on the payment in DB.
	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID + 99999,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		Terminal:       "token",
	}})

	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 0, result.Reconciled)
	assert.Equal(t, 1, result.Failed)

	// The real order should not have been touched.
	flag, status := readOrderState(t, db, ctx, orderID)
	assert.Equal(t, common.OrderFlagToRenew, flag)
	assert.Equal(t, common.OrderStatusPaid, status)
}

func TestReconcileIntegration_AmountMismatch_Failed(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL") // amount=90 NIS

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID,
		PaymentID:      paymentID,
		Amount:         123, // differs from DB
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		Terminal:       "token",
	}})

	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Reconciled)
}

func TestReconcileIntegration_CurrencyMismatch_Failed(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL") // currency=NIS

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyUSD, // differs from DB
		PricingVersion: "v2",
		Terminal:       "token",
	}})

	assert.Equal(t, 1, result.Failed)
}

func TestReconcileIntegration_PricingVersionMismatch_Failed(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL") // pricing_version=v2

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v1", // differs from DB
		Terminal:       "token",
	}})

	assert.Equal(t, 1, result.Failed)
}

func TestReconcileIntegration_FinalizedAsDeclined_RefusesOverwrite(t *testing.T) {
	// Guard: a payment that was finalized as failed (Success="0", Status=failed) must
	// not be flipped to success by reconcile. This state shouldn't normally end up in
	// a reconcile input, but a stale or reprocessed log could contain it, and silently
	// converting a declined charge into a successful one would corrupt financial state.
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL")

	// Simulate a cleanly-finalized decline: PaymentStatus=failed, success=0.
	_, err := db.Exec(ctx,
		`UPDATE payments SET "PaymentStatus"=$1, success=$2 WHERE id=$3`,
		common.PaymentStatusFailed, "0", paymentID,
	)
	require.NoError(t, err)

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		Terminal:       "token",
	}})

	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Reconciled)

	// Payment state must remain declined.
	var payStatus, paySuccess string
	err = db.QueryRow(ctx,
		`SELECT COALESCE("PaymentStatus",''), COALESCE(success,'') FROM payments WHERE id=$1`, paymentID,
	).Scan(&payStatus, &paySuccess)
	require.NoError(t, err)
	assert.Equal(t, common.PaymentStatusFailed, payStatus)
	assert.Equal(t, "0", paySuccess)
}

func TestReconcileIntegration_EventEmitted(t *testing.T) {
	db, ctx := newIntegrationDB(t)
	orderID, paymentID := setupUnfinalizedOrder(t, db, ctx, "IL")

	service := NewBillingService(db, nil, &events.NoopEmitter{}, nil, nil)

	// The test context has an EventBuilder from eventstest.WithTestEventBuilder.
	// The important thing is Reconcile calls emitOrderEvent without panicking.
	result := service.Reconcile(ctx, []ReconcileEntry{{
		OrderID:        orderID,
		PaymentID:      paymentID,
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		Terminal:       "token",
	}})

	assert.Equal(t, 1, result.Reconciled)
}
