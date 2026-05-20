package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pelecardmock "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// failingProfileService returns the configured error for every method.
type failingProfileService struct{ err error }

func (f *failingProfileService) GetProfileByKeycloakID(context.Context, string) (*profiles.Profile, error) {
	return nil, f.err
}

func (f *failingProfileService) LookupProfile(context.Context, string) (*profiles.Profile, error) {
	return nil, f.err
}

func (f *failingProfileService) LookupProfileByKeycloakId(context.Context, string) (*profiles.Profile, error) {
	return nil, f.err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestService(t *testing.T) (*BillingService, *mocks.MockOrdersRepository, *pelecardmock.MockChargeExecutor) {
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	mockExecutor := pelecardmock.NewMockChargeExecutor(t)
	resolver := pricing.NewPriceResolver(nil, nil, nil, "") // not used in charge phase
	emitter := &events.NoopEmitter{}
	service := NewBillingService(mockRepo, mockPelecard, emitter, resolver, mockExecutor)
	return service, mockRepo, mockExecutor
}

// expectProcessOrder sets up mock expectations for a single processOrder call.
// Returns the payment that will be returned from CreateRenewalPayment.
func expectProcessOrder(
	mockRepo *mocks.MockOrdersRepository,
	mockExecutor *pelecardmock.MockChargeExecutor,
	orderID uint,
	terminal pelecard.Terminal,
	gatewayResp map[string]interface{},
	gatewayErr error,
) *repo.Payment {
	pending := &repo.Payment{
		ID:            int(orderID*10 + 1),
		OrderID:       null.IntFrom(int(orderID)),
		Amount:        null.Float64From(80),
		Currency:      null.StringFrom(common.CurrencyNIS),
		PaymentStatus: null.StringFrom(common.PaymentStatusPending),
		ParamX:        null.StringFrom(fmt.Sprintf("m-%d%s", orderID*10+1, terminal.PMX)),
		Ordkey:        null.StringFrom(fmt.Sprintf("ord-%d", orderID)),
		AuthNo:        null.StringFrom("AUTH"),
		PelecardToken: null.StringFrom("TOKEN"),
	}

	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, terminal.PMX).Return(pending, nil).Once()
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, terminal, orderID).
		Return(gatewayResp, gatewayErr).Once()
	mockRepo.EXPECT().FinalizeRenewal(mock.Anything, orderID, mock.Anything).Return(nil).Once()

	return pending
}

// ---------------------------------------------------------------------------
// preResolve
// ---------------------------------------------------------------------------

func TestPreResolve_AllOrdersResolved(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	data.Account.Country = null.StringFrom("GB")
	data.Order.Currency = null.StringFrom(common.CurrencyUSD)

	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(data, nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(2)).Return(data, nil)

	resolved, pricingErrors := service.preResolve(ctx, []uint{1, 2}, 1)

	require.Equal(t, 2, len(resolved))
	assert.Equal(t, "v1", resolved[0].Price.PricingVersion)
	assert.Equal(t, "v1", resolved[1].Price.PricingVersion)
	assert.Equal(t, 0, pricingErrors)
}

func TestPreResolve_V1CountryResolvesSuccessfully(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	data.Account.Country = null.StringFrom("GB") // V1 country (excluded from v2)
	data.Order.Currency = null.StringFrom(common.CurrencyUSD)

	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(data, nil)

	resolved, pricingErrors := service.preResolve(ctx, []uint{1}, 1)

	require.Equal(t, 1, len(resolved))
	assert.Equal(t, uint(1), resolved[0].OrderID)
	assert.Equal(t, "v1", resolved[0].Price.PricingVersion)
	assert.Equal(t, 20.0, resolved[0].Price.Amount)
	assert.Equal(t, 0, pricingErrors)
}

func TestPreResolve_LoadDataError_FlagsPricingError(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(nil, errors.New("db error"))
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagPricingError).Return(nil)

	resolved, pricingErrors := service.preResolve(ctx, []uint{1}, 1)

	assert.Equal(t, 0, len(resolved))
	assert.Equal(t, 1, pricingErrors)
}

func TestPreResolve_PricingError_FlagsPricingError(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	// Override resolver with one whose profile lookup fails — forces V2 pricing to error.
	service.resolver = pricing.NewPriceResolver(
		&failingProfileService{err: errors.New("forced profile error")},
		nil, nil, "",
	)
	ctx := context.Background()

	data := testRenewalData() // already IL + UserKey set

	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(data, nil)
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagPricingError).Return(nil)

	resolved, pricingErrors := service.preResolve(ctx, []uint{1}, 1)

	assert.Equal(t, 0, len(resolved))
	assert.Equal(t, 1, pricingErrors)
}

func TestPreResolve_MixedSuccessAndFailure(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	goodData := testRenewalData()
	goodData.Account.Country = null.StringFrom("GB")
	goodData.Order.Currency = null.StringFrom(common.CurrencyUSD)

	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(goodData, nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(2)).Return(nil, errors.New("not found"))
	mockRepo.EXPECT().FlagOrder(ctx, 2, common.OrderFlagPricingError).Return(nil)

	resolved, pricingErrors := service.preResolve(ctx, []uint{1, 2}, 1)

	assert.Equal(t, 1, len(resolved))
	assert.Equal(t, uint(1), resolved[0].OrderID)
	assert.Equal(t, 1, pricingErrors)
}

func TestPreResolve_FlagOrderError_ContinuesProcessing(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	goodData := testRenewalData()
	goodData.Account.Country = null.StringFrom("GB")
	goodData.Order.Currency = null.StringFrom(common.CurrencyUSD)

	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(nil, errors.New("fail"))
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagPricingError).Return(errors.New("flag failed"))
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(2)).Return(goodData, nil)

	resolved, pricingErrors := service.preResolve(ctx, []uint{1, 2}, 1)

	assert.Equal(t, 1, len(resolved), "should continue after flag error")
	assert.Equal(t, 1, pricingErrors)
}

// ---------------------------------------------------------------------------
// chargeWithPricing
// ---------------------------------------------------------------------------

func TestChargeWithPricing_NoOrders(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	mockRepo.EXPECT().GetOrderIDsToRenew(ctx).Return([]uint{}, nil)

	count, err := service.chargeWithPricing(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestChargeWithPricing_GetOrderIDsError(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	mockRepo.EXPECT().GetOrderIDsToRenew(ctx).Return(nil, errors.New("db down"))

	_, err := service.chargeWithPricing(ctx, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GetOrderIDsToRenew")
}

func TestChargeWithPricing_AllPricingFails(t *testing.T) {
	service, mockRepo, _ := newTestService(t)
	ctx := context.Background()

	mockRepo.EXPECT().GetOrderIDsToRenew(ctx).Return([]uint{1, 2}, nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(nil, errors.New("fail"))
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagPricingError).Return(nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(2)).Return(nil, errors.New("fail"))
	mockRepo.EXPECT().FlagOrder(ctx, 2, common.OrderFlagPricingError).Return(nil)

	count, err := service.chargeWithPricing(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ---------------------------------------------------------------------------
// processWithRecovery — terminal fallback
// ---------------------------------------------------------------------------

func TestProcessWithRecovery_TokenSucceeds(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	price := testV1Price()

	expectProcessOrder(mockRepo, mockExecutor, 100, pelecard.TokenTerminal, gatewaySuccess(), nil)

	stats := newChargeStats(1)

	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.successCount.Get(pelecard.TokenTerminal.Name))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.EMVTerminal.Name))
}

func TestProcessWithRecovery_TokenGatewayError_EMVSucceeds(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	price := testV1Price()

	// Token: gateway error (processOrder returns error + payment with Success="0")
	tokenPending := &repo.Payment{
		ID: 201, OrderID: null.IntFrom(100), PaymentStatus: null.StringFrom(common.PaymentStatusPending),
		Amount: null.Float64From(20), Currency: null.StringFrom(common.CurrencyUSD),
		ParamX: null.StringFrom("m-201t"), Ordkey: null.StringFrom("ord-100"),
		AuthNo: null.StringFrom("A"), PelecardToken: null.StringFrom("T"),
	}
	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, pelecard.TokenTerminal.PMX).Return(tokenPending, nil).Once()
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, pelecard.TokenTerminal, uint(100)).
		Return(nil, errors.New("timeout")).Once()
	mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(100), mock.Anything).Return(nil).Once()

	// EMV: succeeds
	expectProcessOrder(mockRepo, mockExecutor, 100, pelecard.EMVTerminal, gatewaySuccess(), nil)

	stats := newChargeStats(1)

	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.successCount.Get(pelecard.EMVTerminal.Name))
}

func TestProcessWithRecovery_BothGatewayError(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	price := testV1Price()

	for _, terminal := range []pelecard.Terminal{pelecard.TokenTerminal, pelecard.EMVTerminal} {
		p := &repo.Payment{
			ID: 201, OrderID: null.IntFrom(100), PaymentStatus: null.StringFrom(common.PaymentStatusPending),
			Amount: null.Float64From(20), Currency: null.StringFrom(common.CurrencyUSD),
			ParamX: null.StringFrom("m-201"), Ordkey: null.StringFrom("ord-100"),
			AuthNo: null.StringFrom("A"), PelecardToken: null.StringFrom("T"),
		}
		mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, terminal.PMX).Return(p, nil).Once()
		mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, terminal, uint(100)).
			Return(nil, errors.New("timeout")).Once()
		mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(100), mock.Anything).Return(nil).Once()
	}

	stats := newChargeStats(1)

	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("gateway"))
}

// ---------------------------------------------------------------------------
// processWithRecovery — pre/post-payment errors
// ---------------------------------------------------------------------------

func TestProcessWithRecovery_PrePaymentError_StopsRetry(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	price := testV1Price()

	// Token: CreateRenewalPayment fails → wrapped as pre-payment by processOrder
	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, pelecard.TokenTerminal.PMX).
		Return(nil, fmt.Errorf("%w: db error", common.ErrPrePayment)).Once()

	// EMV should NOT be attempted

	stats := newChargeStats(1)

	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("pre_payment"))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.TokenTerminal.Name))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.EMVTerminal.Name))
	mockExecutor.AssertNotCalled(t, "Execute")
}

func TestProcessWithRecovery_PostPaymentError_StopsRetry(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	price := testV1Price()

	pending := testPendingPayment()
	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, pelecard.TokenTerminal.PMX).Return(pending, nil).Once()
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, pelecard.TokenTerminal, uint(100)).
		Return(gatewaySuccess(), nil).Once()
	mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(100), mock.Anything).
		Return(fmt.Errorf("%w: tx commit failed", common.ErrPostPayment)).Once()

	// EMV should NOT be attempted

	stats := newChargeStats(1)

	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("post_payment"))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.EMVTerminal.Name))
}

// ---------------------------------------------------------------------------
// Error wrapping sanity checks
// ---------------------------------------------------------------------------

func TestChargeOperations_ErrorWrapping_PrePayment(t *testing.T) {
	err := fmt.Errorf("%w: o.GetOrderByID: order not found", common.ErrPrePayment)
	assert.True(t, errors.Is(err, common.ErrPrePayment))
	assert.Contains(t, err.Error(), "GetOrderByID")
}

func TestChargeOperations_ErrorWrapping_PostPayment(t *testing.T) {
	err := fmt.Errorf("%w: o.FlagOrderAsRenewed: database locked", common.ErrPostPayment)
	assert.True(t, errors.Is(err, common.ErrPostPayment))
	assert.Contains(t, err.Error(), "FlagOrderAsRenewed")
}

func TestChargeOperations_ErrorWrapping_Gateway(t *testing.T) {
	err := errors.New("payment failed: connection timeout")
	assert.False(t, errors.Is(err, common.ErrPrePayment))
	assert.False(t, errors.Is(err, common.ErrPostPayment))
}

// ---------------------------------------------------------------------------
// processWithRecovery — panic recovery
// ---------------------------------------------------------------------------

// panicChargeExecutor always panics on Execute.
type panicChargeExecutor struct{}

func (p *panicChargeExecutor) Execute(_ context.Context, _ *pelecard.ChargeRequest, _ pelecard.Terminal, _ uint) (map[string]interface{}, error) {
	panic("simulated gateway panic")
}

func TestProcessWithRecovery_PanicRecovery(t *testing.T) {
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	resolver := pricing.NewPriceResolver(nil, nil, nil, "")
	emitter := &events.NoopEmitter{}
	panicExec := &panicChargeExecutor{}
	service := NewBillingService(mockRepo, mockPelecard, emitter, resolver, panicExec)
	ctx := context.Background()

	data := testRenewalData()
	data.Account.Country = null.StringFrom("US")
	price := testV1Price()

	// processOrder will call CreateRenewalPayment, then Execute panics
	pending := testPendingPayment()
	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, pelecard.TokenTerminal.PMX).Return(pending, nil).Once()

	stats := newChargeStats(1)

	// Should NOT panic — recovery catches it
	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("panic"), "panic should be recorded in stats")
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.TokenTerminal.Name))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.EMVTerminal.Name))
}

// ---------------------------------------------------------------------------
// processWithRecovery — mixed outcomes across multiple orders
// ---------------------------------------------------------------------------

func TestProcessWithRecovery_TokenDeclined_EMVPostPaymentError(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	data := testRenewalData()
	price := testV1Price()

	// Token: declined
	expectProcessOrder(mockRepo, mockExecutor, 100, pelecard.TokenTerminal, gatewayDeclined(), nil)

	// EMV: post-payment error (gateway succeeded but DB update failed)
	emvPending := &repo.Payment{
		ID: 202, OrderID: null.IntFrom(100), PaymentStatus: null.StringFrom(common.PaymentStatusPending),
		Amount: null.Float64From(20), Currency: null.StringFrom(common.CurrencyUSD),
		ParamX: null.StringFrom("m-202e"), Ordkey: null.StringFrom("ord-100"),
		AuthNo: null.StringFrom("A"), PelecardToken: null.StringFrom("T"),
	}
	mockRepo.EXPECT().CreateRenewalPayment(mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, pelecard.EMVTerminal.PMX).Return(emvPending, nil).Once()
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, pelecard.EMVTerminal, uint(100)).
		Return(gatewaySuccess(), nil).Once()
	mockRepo.EXPECT().FinalizeRenewal(mock.Anything, uint(100), mock.Anything).
		Return(fmt.Errorf("%w: tx commit failed", common.ErrPostPayment)).Once()

	stats := newChargeStats(1)

	ro := resolvedOrder{OrderID: 100, Data: data, Price: price}
	service.processWithRecovery(ctx, 0, ro, &stats)

	assert.Equal(t, int64(1), stats.errorCount.Get("post_payment"))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.TokenTerminal.Name))
	assert.Equal(t, int64(0), stats.successCount.Get(pelecard.EMVTerminal.Name))
}

// ---------------------------------------------------------------------------
// chargeWithPricing — multi-order with mixed outcomes
// ---------------------------------------------------------------------------

func TestChargeWithPricing_MultipleOrders_MixedOutcomes(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)
	ctx := context.Background()

	// Order 1: succeeds on token
	data1 := testRenewalData()
	data1.Account.Country = null.StringFrom("GB")
	data1.Order.ID = 101

	// Order 2: fails pricing (load data error)
	// Order 3: declined on both terminals

	data3 := testRenewalData()
	data3.Account.Country = null.StringFrom("GB")
	data3.Order.ID = 103

	mockRepo.EXPECT().GetOrderIDsToRenew(ctx).Return([]uint{101, 102, 103}, nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(101)).Return(data1, nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(102)).Return(nil, errors.New("not found"))
	mockRepo.EXPECT().FlagOrder(ctx, 102, common.OrderFlagPricingError).Return(nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(103)).Return(data3, nil)

	// Order 101: token succeeds
	expectProcessOrder(mockRepo, mockExecutor, 101, pelecard.TokenTerminal, gatewaySuccess(), nil)

	// Order 103: both declined
	expectProcessOrder(mockRepo, mockExecutor, 103, pelecard.TokenTerminal, gatewayDeclined(), nil)
	expectProcessOrder(mockRepo, mockExecutor, 103, pelecard.EMVTerminal, gatewayDeclined(), nil)

	count, err := service.chargeWithPricing(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only order 101 should succeed")
}

// ---------------------------------------------------------------------------
// recordPostPaymentError — nil payment edge case
// ---------------------------------------------------------------------------

func TestRecordPostPaymentError_NilPayment_NoPanic(t *testing.T) {
	stats := newChargeStats(1)

	// Should not panic with nil payment
	recordPostPaymentError(
		context.Background(),
		utils.SentryFor(context.Background()),
		&stats,
		"token",
		nil, // nil payment
		errors.New("post-payment error"),
	)

	assert.Equal(t, int64(1), stats.errorCount.Get("post_payment"))
}

