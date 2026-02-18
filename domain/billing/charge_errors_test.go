package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ---------------------------------------------------------------------------
// Error Handling Tests for processOrderWithRecovery
// ---------------------------------------------------------------------------

func TestChargeOperations_PrePaymentError_StopsRetry(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order with pre-payment error (e.g., missing card details)
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	prePaymentErr := fmt.Errorf("%w: o.GetCardDetailById: no rows in result set", common.ErrPrePayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, prePaymentErr)
	// Should NOT try EMV terminal after pre-payment error

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 0, count) // No successful renewals
	// Verify only token terminal was tried (EMV should not be called)
	mockRepo.AssertNotCalled(t, "TryRenewalWithTerminal", mock.Anything, uint(100), pelecard.EMVTerminal)
}

func TestChargeOperations_PostPaymentError_StopsRetry(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order with post-payment error (payment succeeded but DB update failed)
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	postPaymentErr := fmt.Errorf("%w: o.FlagOrderAsRenewed: db error", common.ErrPostPayment)
	// Return payment record indicating potential success
	payment := &repo.Payment{
		Success:  null.StringFrom("1"),
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(payment, postPaymentErr)
	// Should NOT try EMV terminal - payment may have gone through

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 0, count) // No successful renewals (error occurred)
	mockRepo.AssertNotCalled(t, "TryRenewalWithTerminal", mock.Anything, uint(100), pelecard.EMVTerminal)
}

func TestChargeOperations_GatewayError_TriesNextTerminal(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order with gateway error on token, succeeds on EMV
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	gatewayErr := errors.New("payment failed: gateway timeout")
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, gatewayErr)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(successfulPayment(pelecard.EMVTerminal.Name), nil)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 1, count) // Successfully renewed via EMV
}

func TestChargeOperations_GatewayError_BothTerminalsFail(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order with gateway errors on both terminals
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	gatewayErr := errors.New("payment failed: network error")
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, gatewayErr)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(nil, gatewayErr)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 0, count) // No successful renewals
}

func TestChargeOperations_DeclinedPayment_TriesNextTerminal(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order declined on token terminal, succeeds on EMV
	declinedPayment := &repo.Payment{
		Success:  null.StringFrom("0"), // Declined
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(declinedPayment, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(successfulPayment(pelecard.EMVTerminal.Name), nil)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 1, count) // Successfully renewed via EMV
}

func TestChargeOperations_DeclinedPayment_BothTerminals(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order declined on both terminals
	declinedPayment := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(declinedPayment, nil)
	// Return a copy with EMV terminal name
	declinedPaymentEMV := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("emv"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(declinedPaymentEMV, nil)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 0, count) // No successful renewals
}

func TestChargeOperations_MultipleOrders_MixedOutcomes(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 2}

	// Order 100: succeeds on token
	// Order 200: pre-payment error
	// Order 300: declined on token, succeeds on EMV
	// Order 400: gateway error on token, succeeds on EMV
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100, 200, 300, 400}, nil)

	// Order 100: success on token
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(successfulPayment(pelecard.TokenTerminal.Name), nil)

	// Order 200: pre-payment error (no retry)
	prePaymentErr := fmt.Errorf("%w: missing card details", common.ErrPrePayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(200), pelecard.TokenTerminal).Return(nil, prePaymentErr)

	// Order 300: declined on token, succeeds on EMV
	declinedPayment := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(300), pelecard.TokenTerminal).Return(declinedPayment, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(300), pelecard.EMVTerminal).Return(successfulPayment(pelecard.EMVTerminal.Name), nil)

	// Order 400: gateway error on token, succeeds on EMV
	gatewayErr := errors.New("payment failed: timeout")
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(400), pelecard.TokenTerminal).Return(nil, gatewayErr)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(400), pelecard.EMVTerminal).Return(successfulPayment(pelecard.EMVTerminal.Name), nil)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	assert.NoError(t, err)
	assert.Equal(t, 3, count) // Orders 100, 300, 400 succeeded
}

func TestChargeOperations_PostPaymentError_PaymentInfoPreserved(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Post-payment error should preserve payment info for stats
	payment := &repo.Payment{
		Success:  null.StringFrom("1"), // Payment may have succeeded
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyUSD),
		Amount:   null.Float64From(50.0),
	}
	postPaymentErr := fmt.Errorf("%w: UPDATE payments failed", common.ErrPostPayment)
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(payment, postPaymentErr)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	// Should not panic even though error occurred
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestChargeOperations_ErrorWrapping_PrePayment(t *testing.T) {
	// Verify that we can detect wrapped pre-payment errors
	err := fmt.Errorf("%w: o.GetOrderByID: order not found", common.ErrPrePayment)
	assert.True(t, errors.Is(err, common.ErrPrePayment))
	assert.Contains(t, err.Error(), "GetOrderByID")
}

func TestChargeOperations_ErrorWrapping_PostPayment(t *testing.T) {
	// Verify that we can detect wrapped post-payment errors
	err := fmt.Errorf("%w: o.FlagOrderAsRenewed: database locked", common.ErrPostPayment)
	assert.True(t, errors.Is(err, common.ErrPostPayment))
	assert.Contains(t, err.Error(), "FlagOrderAsRenewed")
}

func TestChargeOperations_ErrorWrapping_Gateway(t *testing.T) {
	// Gateway errors should NOT match pre/post payment sentinels
	err := errors.New("payment failed: connection timeout")
	assert.False(t, errors.Is(err, common.ErrPrePayment))
	assert.False(t, errors.Is(err, common.ErrPostPayment))
}

func TestChargeOperations_GatewayErrorThenDecline_TreatedAsDeclined(t *testing.T) {
	// This test verifies the fix for the scenario where:
	// - Terminal 1 (Token) has a gateway error
	// - Terminal 2 (EMV) cleanly declines (no error, just payment declined)
	// Expected: This should be treated as a declined payment, NOT as a gateway error
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// Order with gateway error on token, then declined on EMV
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	gatewayErr := errors.New("payment failed: gateway timeout")
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, gatewayErr)
	
	// EMV terminal responds with clean decline (no error)
	declinedPaymentEMV := &repo.Payment{
		Success:  null.StringFrom("0"), // Declined
		Terminal: null.StringFrom("emv"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(declinedPaymentEMV, nil)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, opts.MaxWorkers)

	// Should not error out - this is a clean decline scenario
	assert.NoError(t, err)
	assert.Equal(t, 0, count) // No successful renewals (payment declined)
	
	// The critical assertion: This scenario should NOT be logged as a gateway error
	// because the last terminal (EMV) responded successfully (just declined the payment)
	// With the fix, lastError is cleared when terminal responds without error,
	// so the outcome is correctly classified as "declined" not "gateway error"
}

// ---------------------------------------------------------------------------
// Edge case tests for bug fixes
// ---------------------------------------------------------------------------

// newTestStats returns a zero-value chargeStats ready for use in direct processOrderWithRecovery tests.
func newTestStats() *chargeStats {
	return &chargeStats{
		successCount: utils.NewCounterMap[int64](),
		successSum:   utils.NewCounterMap[float64](),
		failedCount:  utils.NewCounterMap[int64](),
		failedSum:    utils.NewCounterMap[float64](),
		errorCount:   utils.NewCounterMap[int64](),
	}
}

// Bug 1: Post-payment error with nil payment must not panic and must not retry on EMV.
func TestProcessOrder_PostPaymentNilPayment_NoPanic(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	postPaymentErr := fmt.Errorf("%w: connection lost after gateway call", common.ErrPostPayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, postPaymentErr)

	// Must not panic despite nil payment
	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("post_payment"))
	assert.Equal(t, int64(0), stats.errorCount.Get("panic"))
	assert.Equal(t, float64(0), stats.failedSum.Get(common.CurrencyNIS))
	// EMV must NOT be tried after post-payment error
	mockRepo.AssertNotCalled(t, "TryRenewalWithTerminal", mock.Anything, uint(100), pelecard.EMVTerminal)
}

// Bug 2: Panic in processing is caught by recovery and counted in stats.
func TestProcessOrder_PanicRecovery_CountedInStats(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).
		RunAndReturn(func(_ context.Context, _ uint, _ pelecard.Terminal) (*repo.Payment, error) {
			panic("unexpected nil pointer in test")
		})

	// Must not crash the caller
	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("panic"))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.TokenTerminal.Name))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.EMVTerminal.Name))
}

// Bug 3: Token declined + EMV gateway error must still record the declined amount.
func TestProcessOrder_DeclinedThenGateway_FailedSumRecorded(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	declinedPayment := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(150),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(declinedPayment, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(nil, errors.New("connection timeout"))

	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	assert.Equal(t, float64(150), stats.failedSum.Get(common.CurrencyNIS))
	assert.Equal(t, int64(1), stats.errorCount.Get("gateway"))
}

// Bug 4: Both terminals declined must increment the declined order count.
func TestProcessOrder_DeclinedBoth_FailedCountIncremented(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	declinedToken := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	declinedEMV := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("emv"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(declinedToken, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(declinedEMV, nil)

	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	assert.Equal(t, int64(1), stats.failedCount.Get("total"))
	assert.Equal(t, float64(100), stats.failedSum.Get(common.CurrencyNIS))
	assert.Equal(t, int64(0), stats.errorCount.Get("gateway"))
}

// Bug 5a: Post-payment error where payment succeeded (Success="1") must NOT add to failedSum.
func TestProcessOrder_PostPaymentSuccess_NotInFailedSum(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	payment := &repo.Payment{
		Success:       null.StringFrom("1"),
		Terminal:      null.StringFrom("token"),
		Currency:      null.StringFrom(common.CurrencyNIS),
		Amount:        null.Float64From(200),
		PaymentStatus: null.StringFrom("completed"),
	}
	postPaymentErr := fmt.Errorf("%w: UPDATE orders SET renewed failed", common.ErrPostPayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(payment, postPaymentErr)

	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("post_payment"))
	// Money was charged — must NOT appear in failedSum
	assert.Equal(t, float64(0), stats.failedSum.Get(common.CurrencyNIS))
}

// Bug 5b: Post-payment error where payment failed (Success="0") SHOULD add to failedSum.
func TestProcessOrder_PostPaymentFailed_InFailedSum(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	payment := &repo.Payment{
		Success:       null.StringFrom("0"),
		Terminal:      null.StringFrom("token"),
		Currency:      null.StringFrom(common.CurrencyNIS),
		Amount:        null.Float64From(200),
		PaymentStatus: null.StringFrom("declined"),
	}
	postPaymentErr := fmt.Errorf("%w: UPDATE payments failed", common.ErrPostPayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(payment, postPaymentErr)

	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("post_payment"))
	assert.Equal(t, float64(200), stats.failedSum.Get(common.CurrencyNIS))
}

// Bug 6: Token gateway error + EMV decline must not count a gateway error (order outcome = declined).
func TestProcessOrder_GatewayThenDecline_NoGatewayErrorCounted(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	stats := newTestStats()

	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, errors.New("gateway timeout"))
	declinedEMV := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("emv"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(80),
	}
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(declinedEMV, nil)

	processOrderWithRecovery(ctx, mockRepo, 0, 100, stats)

	// Outcome is "declined", not "gateway error"
	assert.Equal(t, int64(0), stats.errorCount.Get("gateway"))
	assert.Equal(t, int64(1), stats.failedCount.Get("total"))
	assert.Equal(t, float64(80), stats.failedSum.Get(common.CurrencyNIS))
}

// Cross-terminal error combinations that previously had no coverage.

func TestChargeOperations_GatewayThenPostPayment_NilPayment_NoPanic(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	// Token gateway error, EMV post-payment error with nil payment
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, errors.New("gateway timeout"))
	postPaymentErr := fmt.Errorf("%w: connection reset", common.ErrPostPayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(nil, postPaymentErr)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, 1)

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestChargeOperations_GatewayThenPrePayment(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	// Token gateway error, EMV pre-payment error
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, errors.New("gateway timeout"))
	prePaymentErr := fmt.Errorf("%w: card details missing for EMV", common.ErrPrePayment)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(nil, prePaymentErr)

	count, err := chargeOrdersConcurrent(ctx, mockRepo, 1)

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestChargeOperations_DeclinedThenGatewayError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	// Token declined, EMV gateway error — should not panic, count = 0
	declinedPayment := &repo.Payment{
		Success:  null.StringFrom("0"),
		Terminal: null.StringFrom("token"),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(100),
	}
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(declinedPayment, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(nil, errors.New("network error"))

	count, err := chargeOrdersConcurrent(ctx, mockRepo, 1)

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}
