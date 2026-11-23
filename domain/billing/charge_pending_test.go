package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/internal/mocks"
	pelecardmock "github.com/Bnei-Baruch/vh-srv-orders/internal/mocks/pkg"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/pelecard"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

var errUnknownCharge = fmt.Errorf("%w: external_payments answered pending", pelecard.ErrOutcomeUnknown)

// expectUnknownCharge is processOrder on a charge with no known outcome: the
// payment is created and charged, then held — MarkRenewalChargePending and no
// FinalizeRenewal (the mock fails the test on an unexpected call).
func expectUnknownCharge(mockRepo *mocks.MockOrdersRepository, mockExecutor *pelecardmock.MockChargeExecutor,
	orderID uint, terminal pelecard.Terminal, markErr error) {
	pending := &repo.Payment{
		ID: int(orderID*10 + 2), OrderID: null.IntFrom(int(orderID)),
		PaymentStatus: null.StringFrom(common.PaymentStatusPending),
		ParamX:        null.StringFrom(fmt.Sprintf("m-%d%s", orderID*10+2, terminal.PMX)),
		Ordkey:        null.StringFrom(fmt.Sprintf("ord-%d", orderID)),
	}
	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, terminal.PMX).Return(pending, nil).Once()
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, terminal, orderID).
		Return(map[string]interface{}{"status": "pending"}, errUnknownCharge).Once()
	mockRepo.EXPECT().MarkRenewalChargePending(mock.Anything, orderID).Return(markErr).Once()
}

// The token charge's outcome is unknown: the EMV leg must not run, since the
// card may already have been charged. The order is held charge_pending and
// counted as unknown, neither collected nor declined.
func TestProcessWithRecovery_TokenOutcomeUnknown_HeldWithoutEMV(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	expectUnknownCharge(mockRepo, mockExecutor, 100, pelecard.TokenTerminal, nil)
	// No EMV expectations: a CreateRenewalPayment or Execute on EMV fails the test.

	stats := newChargeStats(1)
	credential := service.processWithRecovery(context.Background(), 0,
		resolvedOrder{OrderID: 100, Data: testRenewalData(), Price: testV1Price()}, &stats)

	assert.False(t, credential)
	assert.Equal(t, int64(1), stats.errorCount.Get("outcome_unknown"))
	assert.Equal(t, 20.0, stats.reasonFailedSum.Get("outcome_unknown:"+common.CurrencyUSD))
	assert.Zero(t, stats.successCount.Get(pelecard.TokenTerminal.Name)+stats.successCount.Get(pelecard.EMVTerminal.Name))
	assert.Zero(t, stats.failedCount.Get("total"), "an unknown outcome is not a decline")
	assert.Zero(t, stats.errorCount.Get("gateway"))
}

// Token declined, then the EMV charge's outcome is unknown: held the same way.
func TestProcessWithRecovery_TokenDeclined_EMVOutcomeUnknown_Held(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	expectProcessOrder(mockRepo, mockExecutor, 100, pelecard.TokenTerminal, gatewayDeclined(), nil)
	expectUnknownCharge(mockRepo, mockExecutor, 100, pelecard.EMVTerminal, nil)

	stats := newChargeStats(1)
	service.processWithRecovery(context.Background(), 0,
		resolvedOrder{OrderID: 100, Data: testRenewalData(), Price: testV1Price()}, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("outcome_unknown"))
	assert.Zero(t, stats.failedCount.Get("total"))
	assert.Zero(t, stats.errorCount.Get("gateway"))
}

// Holding the order failed, so it is still torenew. The EMV leg must still not
// run: the charge may have gone through regardless of what the flag says.
func TestProcessWithRecovery_OutcomeUnknownNotHeld_StillNoEMV(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	expectUnknownCharge(mockRepo, mockExecutor, 100, pelecard.TokenTerminal, errors.New("db down"))

	stats := newChargeStats(1)
	service.processWithRecovery(context.Background(), 0,
		resolvedOrder{OrderID: 100, Data: testRenewalData(), Price: testV1Price()}, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("outcome_unknown"))
}

// processOrder hands the unknown outcome back wrapped, with the payment left
// pending: not marked failed, not finalized.
func TestProcessOrder_OutcomeUnknown_LeavesPaymentPending(t *testing.T) {
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockExecutor := pelecardmock.NewMockChargeExecutor(t)
	expectUnknownCharge(mockRepo, mockExecutor, 100, pelecard.TokenTerminal, nil)

	payment, err := processOrder(context.Background(), mockRepo, &events.NoopEmitter{}, mockExecutor,
		testRenewalData(), testV1Price(), pelecard.TokenTerminal)

	require.ErrorIs(t, err, pelecard.ErrOutcomeUnknown)
	require.NotNil(t, payment)
	assert.Equal(t, common.PaymentStatusPending, payment.PaymentStatus.String)
	assert.False(t, payment.Success.Valid, "success is not decided")
}

// ---------------------------------------------------------------------------
// ReconcilePendingCharges
// ---------------------------------------------------------------------------

func newReconcileService(t *testing.T) (*BillingService, *mocks.MockOrdersRepository, *pelecardmock.MockPelecardAPI) {
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	return NewBillingService(mockRepo, mockPelecard, &events.NoopEmitter{}, nil, nil), mockRepo, mockPelecard
}

func heldPayment() *repo.Payment {
	return &repo.Payment{
		ID: 7, OrderID: null.IntFrom(100), Amount: null.Float64From(20),
		PaymentStatus: null.StringFrom(common.PaymentStatusPending),
		ParamX:        null.StringFrom("m-7t"), Ordkey: null.StringFrom("ord-100"),
	}
}

func expectHeld(mockRepo *mocks.MockOrdersRepository) {
	mockRepo.EXPECT().GetChargePendingRenewals(mock.Anything, mock.Anything).
		Return([]repo.PendingCharge{{OrderID: 100, PaymentID: 7}}, nil).Once()
	mockRepo.EXPECT().GetPaymentByID(mock.Anything, 7).Return(heldPayment(), nil).Once()
}

// external_payments found the charge (valid): the renewal finishes as a
// success, exactly as if the charge had answered success.
func TestReconcilePendingCharges_ValidIsCharged(t *testing.T) {
	service, mockRepo, mockPelecard := newReconcileService(t)
	expectHeld(mockRepo)
	mockPelecard.EXPECT().GetChargeStatus(mock.Anything, "ord-100", "m-7t").Return("valid", nil).Once()
	mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(100), mock.MatchedBy(func(p *repo.Payment) bool {
		return p.ID == 7 && p.Success.String == "1" && p.PaymentStatus.String == common.PaymentStatusSuccess
	})).Return(nil).Once()

	result, err := service.ReconcilePendingCharges(context.Background(), 30*time.Minute, false)

	require.NoError(t, err)
	assert.Equal(t, ReconcilePendingResult{Checked: 1, Charged: 1}, result)
}

// Not charged — refused, cancelled by its reconciliation, or never stored: the
// payment fails (FinalizeRenewal also returns the order to torenew).
func TestReconcilePendingCharges_NotChargedFails(t *testing.T) {
	cases := map[string]struct {
		status string
		err    error
	}{
		"invalid":         {status: "invalid"},
		"cancel":          {status: "cancel"},
		"error":           {status: "error"},
		"no such request": {err: pelecard.ErrPaymentNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			service, mockRepo, mockPelecard := newReconcileService(t)
			expectHeld(mockRepo)
			mockPelecard.EXPECT().GetChargeStatus(mock.Anything, "ord-100", "m-7t").Return(tc.status, tc.err).Once()
			mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(100), mock.MatchedBy(func(p *repo.Payment) bool {
				return p.Success.String == "0" && p.PaymentStatus.String == common.PaymentStatusFailed
			})).Return(nil).Once()

			result, err := service.ReconcilePendingCharges(context.Background(), 30*time.Minute, false)

			require.NoError(t, err)
			assert.Equal(t, ReconcilePendingResult{Checked: 1, NotCharge: 1}, result)
		})
	}
}

// Still new or in-process at external_payments: nothing is written, the order
// stays held for a later run.
func TestReconcilePendingCharges_UnsettledStaysHeld(t *testing.T) {
	for _, status := range []string{"new", "in-process"} {
		t.Run(status, func(t *testing.T) {
			service, mockRepo, mockPelecard := newReconcileService(t)
			expectHeld(mockRepo)
			mockPelecard.EXPECT().GetChargeStatus(mock.Anything, "ord-100", "m-7t").Return(status, nil).Once()

			result, err := service.ReconcilePendingCharges(context.Background(), 30*time.Minute, false)

			require.NoError(t, err)
			assert.Equal(t, ReconcilePendingResult{Checked: 1, Unsettled: 1}, result)
		})
	}
}

// A failed lookup is not an answer: counted as an error, nothing written, and
// the run goes on to the next order.
func TestReconcilePendingCharges_LookupFailureWritesNothing(t *testing.T) {
	service, mockRepo, mockPelecard := newReconcileService(t)
	mockRepo.EXPECT().GetChargePendingRenewals(mock.Anything, mock.Anything).
		Return([]repo.PendingCharge{{OrderID: 100, PaymentID: 7}, {OrderID: 200, PaymentID: 8}}, nil).Once()
	mockRepo.EXPECT().GetPaymentByID(mock.Anything, 7).Return(heldPayment(), nil).Once()
	second := heldPayment()
	second.ID, second.OrderID, second.ParamX, second.Ordkey = 8, null.IntFrom(200), null.StringFrom("m-8t"), null.StringFrom("ord-200")
	mockRepo.EXPECT().GetPaymentByID(mock.Anything, 8).Return(second, nil).Once()
	mockPelecard.EXPECT().GetChargeStatus(mock.Anything, "ord-100", "m-7t").Return("", errors.New("timeout")).Once()
	mockPelecard.EXPECT().GetChargeStatus(mock.Anything, "ord-200", "m-8t").Return("valid", nil).Once()
	mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(200), mock.Anything).Return(nil).Once()

	result, err := service.ReconcilePendingCharges(context.Background(), 30*time.Minute, false)

	require.NoError(t, err)
	assert.Equal(t, ReconcilePendingResult{Checked: 2, Charged: 1, Errors: 1}, result)
}

// With dryRun the outcome is reported and nothing is written.
func TestReconcilePendingCharges_DryRunWritesNothing(t *testing.T) {
	service, mockRepo, mockPelecard := newReconcileService(t)
	expectHeld(mockRepo)
	mockPelecard.EXPECT().GetChargeStatus(mock.Anything, "ord-100", "m-7t").Return("valid", nil).Once()

	result, err := service.ReconcilePendingCharges(context.Background(), 30*time.Minute, true)

	require.NoError(t, err)
	assert.Equal(t, ReconcilePendingResult{Checked: 1, Charged: 1}, result)
}

// The cutoff handed to the repo is now minus olderThan, so a charge younger
// than that is left for external_payments' own reconciliation.
func TestReconcilePendingCharges_AsksOnlyForChargesOlderThanTheCutoff(t *testing.T) {
	service, mockRepo, _ := newReconcileService(t)
	before := time.Now()
	mockRepo.EXPECT().GetChargePendingRenewals(mock.Anything, mock.MatchedBy(func(cutoff time.Time) bool {
		return !cutoff.After(before.Add(-2*time.Hour).Add(time.Second)) && cutoff.After(before.Add(-2*time.Hour).Add(-time.Minute))
	})).Return([]repo.PendingCharge{}, nil).Once()

	result, err := service.ReconcilePendingCharges(context.Background(), 2*time.Hour, false)

	require.NoError(t, err)
	assert.Equal(t, ReconcilePendingResult{}, result)
}
