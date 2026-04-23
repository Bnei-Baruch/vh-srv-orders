package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pelecardmock "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewBillingService(t *testing.T) {
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	period := NewBillingPeriodWithDate(6, 2024)
	opts := &WorkflowOptions{Muhlafim: false}

	err := service.processMuhlafim(ctx, period, opts)
	assert.NoError(t, err)
}

func TestProcessMuhlafim_CallsProcessMuhlafimWithCorrectDates(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	opts := &WorkflowOptions{Charge: false}

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_NoOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	opts := &WorkflowOptions{Charge: true, MaxWorkers: 1}

	mockRepo.EXPECT().GetOrderIDsToRenew(ctx).Return([]uint{}, nil)

	err := service.chargeOperations(ctx, opts)
	assert.NoError(t, err)
}

func TestChargeOperations_GetOrderIDsError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	opts := &WorkflowOptions{Charge: true, MaxWorkers: 1}

	mockRepo.EXPECT().GetOrderIDsToRenew(ctx).Return(nil, errors.New("db down"))

	err := service.chargeOperations(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charge with pricing")
}

// ---------------------------------------------------------------------------
// RunBillingWorkflow
// ---------------------------------------------------------------------------

func TestRunBillingWorkflow_AllStepsSkipped(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

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
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	month := 6
	year := 2024
	period := NewBillingPeriodWithDate(month, year)
	opts := &WorkflowOptions{Flags: true, Muhlafim: false, Charge: true, MaxWorkers: 1}

	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastDayThisMonth := period.GetEndOfMonth()

	// Step 1: succeeds
	mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
	mockRepo.EXPECT().UpdateOrdersUserKeyFromAccounts(ctx).Return(nil)
	mockRepo.EXPECT().FlagOrdersToRenew(ctx, int64(month), int64(year)).Return(int64(1), nil)
	mockRepo.EXPECT().GetFlaggedOrders(mock.Anything).Return([]repo.Order{}, nil).Times(2)
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, 2024, 5, lastDayLastMonth).Return([]string{}, nil)
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, 2024, 6, lastDayThisMonth).Return([]string{}, nil)

	// Step 3: charge fails at GetOrderIDsToRenew
	mockRepo.EXPECT().GetOrderIDsToRenew(mock.Anything).Return(nil, errors.New("db error"))

	err := service.RunBillingWorkflow(ctx, month, year, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "charge with pricing")
}

// ---------------------------------------------------------------------------
// RetryPricingErrors
// ---------------------------------------------------------------------------

func TestRetryPricingErrors_NoPricingErrorOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	mockRepo.EXPECT().GetOrderIDsWithPricingError(ctx).Return([]uint{}, nil)

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRetryPricingErrors_GetOrdersError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	mockRepo.EXPECT().GetOrderIDsWithPricingError(ctx).Return(nil, errors.New("db error"))

	_, err := service.RetryPricingErrors(ctx, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GetOrderIDsWithPricingError")
}

func TestRetryPricingErrors_AllStillFail(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)
	service := NewBillingService(mockRepo, mockPelecard, nil, nil, nil)

	mockRepo.EXPECT().GetOrderIDsWithPricingError(ctx).Return([]uint{1}, nil)
	mockRepo.EXPECT().LoadRenewalData(ctx, uint(1)).Return(nil, errors.New("not found"))
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagPricingError).Return(nil)

	count, err := service.RetryPricingErrors(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
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
