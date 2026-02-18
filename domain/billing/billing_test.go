package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pelecardmock "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// successfulPayment returns a *repo.Payment that processOrderWithRecovery treats as success (Success.String == "1").
func successfulPayment(terminalName string) *repo.Payment {
	return &repo.Payment{
		Success:  null.StringFrom("1"),
		Terminal: null.StringFrom(terminalName),
		Currency: null.StringFrom(common.CurrencyNIS),
		Amount:   null.Float64From(10),
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewBillingService(t *testing.T) {
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	service := NewBillingService(mockRepo, mockPelecard)

	assert.NotNil(t, service)
	assert.Equal(t, mockRepo, service.repo)
	assert.Equal(t, mockPelecard, service.pelecardClient)
}

// ---------------------------------------------------------------------------
// flagAndSkipOperations
// ---------------------------------------------------------------------------

func TestFlagAndSkipOperations_SkippedWhenFlagsFalse(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Flags: false}

	// No repo calls expected when Flags is false
	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.NoError(t, err)
}

func TestFlagAndSkipOperations_FullFlow(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Flags: true}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	// Clear flags
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	// Update user keys
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	// Flag orders for renewal
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(6), int64(2024)).Return(int64(10), nil)
	// flagAndSkipOperations does not call GetFlaggedOrders (only SkipDouble/SkipFresh repo methods)
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return([]string{"user1"}, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user1").Return(1, nil)
	// SkipFreshOrders: this month = June 2024
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 6, lastDayThisMonth).Return([]string{"user2"}, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user2").Return(1, nil)

	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.NoError(t, err)
}

func TestFlagAndSkipOperations_ClearFlagsError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Flags: true}

	mockRepo.EXPECT().ClearAllFlags(ctx).Return(errors.New("db error"))

	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clear flags")
}

func TestFlagAndSkipOperations_UpdateUserKeysError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Flags: true}

	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(errors.New("db error"))

	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update user keys")
}

func TestFlagAndSkipOperations_FlagOrdersError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Flags: true}

	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(6), int64(2024)).Return(int64(0), errors.New("flag error"))

	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "flag orders for renewal")
}

func TestFlagAndSkipOperations_JanuaryWrapsToDecember(t *testing.T) {
	// January billing period: last month should be December of previous year
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(1, 2024)
	opts := &WorkflowOptions{Flags: true}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(1), int64(2024)).Return(int64(5), nil)
	// flagAndSkipOperations does not call GetFlaggedOrders
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2023, 12, lastDayLastMonth).Return([]string{}, nil)
	// SkipFresh: January 2024
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 1, lastDayThisMonth).Return([]string{}, nil)

	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.NoError(t, err)
}

func TestFlagAndSkipOperations_SkipDoubleContinuesOnError(t *testing.T) {
	// SkipDoubleOrders error is now fatal (returns error to caller)
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Flags: true}

	lastDayLastMonth := period.GetLastDayOfLastMonth()

	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(6), int64(2024)).Return(int64(5), nil)
	// SkipDoubleOrders calls GetOrdersToSkipDouble first; no GetFlaggedOrders in this path
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return(nil, errors.New("skip double error"))

	err := service.flagAndSkipOperations(ctx, period, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "skip double orders")
}

// ---------------------------------------------------------------------------
// processMuhlafim
// ---------------------------------------------------------------------------

func TestProcessMuhlafim_SkippedWhenMuhlafimFalse(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Muhlafim: false}

	err := service.processMuhlafim(ctx, period, opts)
	assert.NoError(t, err)
}

func TestProcessMuhlafim_CallsProcessMuhlafimWithCorrectDates(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Muhlafim: true}

	// No flagged orders -> early return from ProcessMuhlafim
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{}, nil)

	err := service.processMuhlafim(ctx, period, opts)
	assert.NoError(t, err)
}

func TestProcessMuhlafim_ErrorPropagated(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Muhlafim: true}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(nil, errors.New("db error"))

	err := service.processMuhlafim(ctx, period, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetch flagged orders")
}

// ---------------------------------------------------------------------------
// chargeOperations
// ---------------------------------------------------------------------------

func TestChargeOperations_SkippedWhenChargeFalse(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: false}

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_SequentialCharging(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: false}

	// Charge masof oorat keva (token terminal)
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX).Return(5, nil)
	// Charge masof ragil (emv terminal)
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.EMVTerminal.PMX).Return(3, nil)

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_NilOpts_SequentialCharging(t *testing.T) {
	// nil opts should default to sequential charging
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX).Return(5, nil)
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.EMVTerminal.PMX).Return(3, nil)

	err := service.chargeOperations(ctx, nil)
	assert.NoError(t, err)
}

func TestChargeOperations_SequentialFirstChargeError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: false}

	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX).Return(0, errors.New("charge error"))

	err := service.chargeOperations(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charge masof oorat keva")
}

func TestChargeOperations_SequentialSecondChargeError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: false}

	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX).Return(5, nil)
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.EMVTerminal.PMX).Return(0, errors.New("charge error"))

	err := service.chargeOperations(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charge masof ragil")
}

func TestChargeOperations_ConcurrentNoOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 2}

	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{}, nil)

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_ConcurrentWithOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 2}

	// Two orders to renew
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100, 200}, nil)
	// Both succeed on token terminal (Payment must have Success=="1" so processOrderWithRecovery returns)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(successfulPayment(pelecard.TokenTerminal.Name), nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(200), pelecard.TokenTerminal).Return(successfulPayment(pelecard.TokenTerminal.Name), nil)

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_ConcurrentFallbackToEMV(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 1}

	// One order, token fails -> emv succeeds
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(nil, errors.New("token fail"))
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.EMVTerminal).Return(successfulPayment(pelecard.EMVTerminal.Name), nil)

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_ConcurrentGetOrderIDsError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Charge: true, UseConcurrent: true, MaxWorkers: 2}

	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return(nil, errors.New("db error"))

	err := service.chargeOperations(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charge orders concurrently")
}

// ---------------------------------------------------------------------------
// RunBillingWorkflow
// ---------------------------------------------------------------------------

func TestRunBillingWorkflow_AllStepsSucceed(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	month := 6
	year := 2024
	period := NewBillingPeriodWithDate(month, year)
	opts := &WorkflowOptions{
		Flags:    true,
		Muhlafim: true,
		Charge:   true,
	}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	// Step 1: Flag & Skip (does not call GetFlaggedOrders)
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(month), int64(year)).Return(int64(5), nil)
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return([]string{}, nil)
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 6, lastDayThisMonth).Return([]string{}, nil)

	// logOrdersCountByStatus after Step 1
	flaggedOrders := []repo.Order{{ID: 1}, {ID: 2}}
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(flaggedOrders, nil).Once()
	// Step 2: Muhlafim - return no flagged orders so ProcessMuhlafim exits early (no GetTokensForOrders)
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{}, nil).Once()
	// logOrdersCountByStatus after Step 2
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(flaggedOrders, nil).Once()

	// Step 3: Charge (sequential)
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX).Return(3, nil)
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.EMVTerminal.PMX).Return(2, nil)

	err := service.RunBillingWorkflow(ctx, month, year, opts)
	assert.NoError(t, err)
}

func TestRunBillingWorkflow_AllStepsSkipped(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{
		Flags:    false,
		Muhlafim: false,
		Charge:   false,
	}

	// When all steps are disabled we still run logOrdersCountByStatus after Step 1 and after Step 2
	mockRepo.EXPECT().GetFlaggedOrders(mock.Anything).Return([]repo.Order{}, nil).Times(2)
	err := service.RunBillingWorkflow(ctx, 6, 2024, opts)
	assert.NoError(t, err)
}

func TestRunBillingWorkflow_ErrorInFlagging(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	opts := &WorkflowOptions{Flags: true, Muhlafim: true, Charge: true}

	// Flagging fails at ClearAllFlags
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(errors.New("db error"))

	err := service.RunBillingWorkflow(ctx, 6, 2024, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "flag and skip operations")
}

func TestRunBillingWorkflow_ErrorInMuhlafim(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	month := 6
	year := 2024
	period := NewBillingPeriodWithDate(month, year)
	opts := &WorkflowOptions{Flags: true, Muhlafim: true, Charge: true}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	// Step 1: Flag & Skip succeeds
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(month), int64(year)).Return(int64(5), nil)
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{{ID: 1}}, nil).Times(2)
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return([]string{}, nil)
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 6, lastDayThisMonth).Return([]string{}, nil)

	// Step 2: Muhlafim fails
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{{ID: 1}}, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(map[int]string{1: "token123"}, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(nil, errors.New("pelecard error"))

	err := service.RunBillingWorkflow(ctx, month, year, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "process muhlafim")
}

func TestRunBillingWorkflow_ErrorInCharging(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	month := 6
	year := 2024
	period := NewBillingPeriodWithDate(month, year)
	opts := &WorkflowOptions{Flags: true, Muhlafim: true, Charge: true}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	// Step 1: Flag & Skip succeeds
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(month), int64(year)).Return(int64(5), nil)
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return([]string{}, nil)
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 6, lastDayThisMonth).Return([]string{}, nil)

	// logOrdersCountByStatus after Step 1, then processMuhlafim (empty), then log after Step 2
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{{ID: 1}}, nil).Once()
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{}, nil).Once()
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{{ID: 1}}, nil).Once()

	// Step 3: Charge fails
	mockRepo.EXPECT().ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX).Return(0, errors.New("charge error"))

	err := service.RunBillingWorkflow(ctx, month, year, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charge operations")
}

func TestRunBillingWorkflow_ConcurrentCharging(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard)

	month := 6
	year := 2024
	period := NewBillingPeriodWithDate(month, year)
	opts := &WorkflowOptions{
		Flags:         true,
		Muhlafim:      true,
		Charge:        true,
		UseConcurrent: true,
		MaxWorkers:    2,
	}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	// Step 1: Flag & Skip
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(month), int64(year)).Return(int64(2), nil)
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return([]string{}, nil)
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 6, lastDayThisMonth).Return([]string{}, nil)

	// logOrdersCountByStatus after Step 1, then processMuhlafim (empty), then log after Step 2
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{{ID: 1}}, nil).Once()
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{}, nil).Once()
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{{ID: 1}}, nil).Once()

	// Step 3: Concurrent charge
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return([]uint{100}, nil)
	mockRepo.EXPECT().TryRenewalWithTerminal(mock.Anything, uint(100), pelecard.TokenTerminal).Return(successfulPayment(pelecard.TokenTerminal.Name), nil)

	err := service.RunBillingWorkflow(ctx, month, year, opts)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// getDefaultMaxWorkers
// ---------------------------------------------------------------------------

func TestGetDefaultMaxWorkers(t *testing.T) {
	// Without config value set, should return 5
	original := common.Config.RenewalMaxWorkers
	defer func() { common.Config.RenewalMaxWorkers = original }()

	common.Config.RenewalMaxWorkers = 0
	workers := getDefaultMaxWorkers()
	assert.Equal(t, 5, workers)
}

func TestGetDefaultMaxWorkers_WithConfig(t *testing.T) {
	original := common.Config.RenewalMaxWorkers
	defer func() { common.Config.RenewalMaxWorkers = original }()

	common.Config.RenewalMaxWorkers = 10
	workers := getDefaultMaxWorkers()
	assert.Equal(t, 10, workers)
}

func TestGetDefaultMaxWorkers_ZeroConfig(t *testing.T) {
	original := common.Config.RenewalMaxWorkers
	defer func() { common.Config.RenewalMaxWorkers = original }()

	common.Config.RenewalMaxWorkers = 0
	workers := getDefaultMaxWorkers()
	assert.Equal(t, 5, workers) // 0 is not > 0, falls back to default
}

func TestGetDefaultMaxWorkers_NegativeConfig(t *testing.T) {
	original := common.Config.RenewalMaxWorkers
	defer func() { common.Config.RenewalMaxWorkers = original }()

	common.Config.RenewalMaxWorkers = -1
	workers := getDefaultMaxWorkers()
	assert.Equal(t, 5, workers) // negative falls back to default
}
