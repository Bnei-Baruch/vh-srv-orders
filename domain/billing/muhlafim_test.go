package billing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pelecardmock "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func TestProcessMuhlafim_NoFlaggedOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return([]repo.Order{}, nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Updated)
	assert.Equal(t, 0, result.NewCards)
}

func TestProcessMuhlafim_NoTokens(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{
		{ID: 1},
		{ID: 2},
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1, 2}).Return(map[int]string{}, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(map[string]pelecard.MuhlafimEntry{}, nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.Equal(t, 0, result.Processed)
}

func TestProcessMuhlafim_WithNewCard(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{
		{ID: 1},
	}

	tokenMap := map[int]string{
		1: "token123",
	}

	muhlafimData := map[string]pelecard.MuhlafimEntry{
		"token123": {
			Token:             "token123",
			ActionDescription: "test",
			NewCardNumber:     "1234567890123456",
			NewExpirationDate: "12/25",
		},
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(muhlafimData, nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 0, result.Updated)
	assert.Equal(t, 1, result.NewCards)
}

func TestProcessMuhlafim_WithFlagUpdate(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{
		{ID: 1},
	}

	tokenMap := map[int]string{
		1: "token123",
	}

	muhlafimData := map[string]pelecard.MuhlafimEntry{
		"token123": {
			Token:             "token123",
			ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
			NewCardNumber:     "",
			NewExpirationDate: "",
		},
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(muhlafimData, nil)
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagMuhHiyuvNiklat).Return(nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 1, result.Updated)
	assert.Equal(t, 0, result.NewCards)
}

func TestProcessMuhlafim_AllActionTypes(t *testing.T) {
	testCases := []struct {
		name            string
		actionDesc      string
		expectedFlag    string
	}{
		{"MUH_HIYUV_NIKLAT", pelecard.MUH_HIYUV_NIKLAT, common.OrderFlagMuhHiyuvNiklat},
		{"MUH_NIDHA", pelecard.MUH_NIDHA, common.OrderFlagMuhNidha},
		{"MUH_BITUL", pelecard.MUH_BITUL, common.OrderFlagMuhBitul},
		{"MUH_LOTAKIN", pelecard.MUH_LOTAKIN, common.OrderFlagMuhLotakin},
		{"Unknown", "unknown_action", common.OrderFlagMuhAher},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			mockRepo := mocks.NewMockOrdersRepository(t)
			mockPelecard := pelecardmock.NewMockPelecardAPI(t)

			orders := []repo.Order{{ID: 1}}
			tokenMap := map[int]string{1: "token123"}
			muhlafimData := map[string]pelecard.MuhlafimEntry{
				"token123": {
					Token:             "token123",
					ActionDescription: tc.actionDesc,
					NewCardNumber:     "",
				},
			}

			mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
			mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
			mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(muhlafimData, nil)
			mockRepo.EXPECT().FlagOrder(ctx, 1, tc.expectedFlag).Return(nil)

			result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

			assert.NoError(t, err)
			assert.Equal(t, 1, result.Processed)
			assert.Equal(t, 1, result.Updated)
		})
	}
}

func TestProcessMuhlafim_MultipleOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{
		{ID: 1},
		{ID: 2},
		{ID: 3},
	}

	tokenMap := map[int]string{
		1: "token1",
		2: "token2",
		3: "token3",
	}

	muhlafimData := map[string]pelecard.MuhlafimEntry{
		"token1": {
			Token:             "token1",
			ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
			NewCardNumber:     "",
		},
		"token2": {
			Token:             "token2",
			ActionDescription: "",
			NewCardNumber:     "1234567890123456",
		},
		// token3 not in muhlafimData - should be skipped
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1, 2, 3}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(muhlafimData, nil)
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagMuhHiyuvNiklat).Return(nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.Equal(t, 2, result.Processed) // Only token1 and token2 are processed
	assert.Equal(t, 1, result.Updated)   // Only token1 gets flag updated
	assert.Equal(t, 1, result.NewCards) // token2 has new card
}

func TestProcessMuhlafim_ErrorFetchingFlaggedOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	expectedErr := errors.New("database error")
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(nil, expectedErr)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "fetch flagged orders with tokens")
}

func TestProcessMuhlafim_ErrorFetchingTokens(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{{ID: 1}}

	expectedErr := errors.New("database error")
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(nil, expectedErr)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "fetch flagged orders with tokens")
}

func TestProcessMuhlafim_ErrorFetchingMuhlafimData(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{{ID: 1}}
	tokenMap := map[int]string{1: "token123"}

	expectedErr := errors.New("pelecard error")
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(nil, expectedErr)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "fetch muhlafim data")
}

func TestProcessMuhlafim_ErrorFlaggingOrder(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{{ID: 1}}
	tokenMap := map[int]string{1: "token123"}
	muhlafimData := map[string]pelecard.MuhlafimEntry{
		"token123": {
			Token:             "token123",
			ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
			NewCardNumber:     "",
		},
	}

	expectedErr := errors.New("flag error")
	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(muhlafimData, nil)
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagMuhHiyuvNiklat).Return(expectedErr)

	// Should continue processing and not fail the entire operation
	// Note: When flagging fails, processOrderWithMuhlafim returns an error,
	// and ProcessMuhlafim continues without adding stats, so processed count is 0
	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err) // Individual order errors don't fail the whole operation
	// When an error occurs, we continue but don't count it as processed
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Updated) // Update failed
}

func TestProcessMuhlafim_WithSaveToFile(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{{ID: 1}}
	tokenMap := map[int]string{1: "token123"}
	muhlafimData := map[string]pelecard.MuhlafimEntry{
		"token123": {
			Token:             "token123",
			ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
			NewCardNumber:     "",
		},
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(muhlafimData, nil)
	mockRepo.EXPECT().FlagOrder(ctx, 1, common.OrderFlagMuhHiyuvNiklat).Return(nil)

	// Clean up test file if it exists
	defer os.Remove("muhlafim.json")

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), true, false)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.Processed)

	// Verify file was created
	_, err = os.Stat("muhlafim.json")
	assert.NoError(t, err, "muhlafim.json should be created")
}

func TestProcessMuhlafim_EmptyToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{{ID: 1}}
	tokenMap := map[int]string{
		1: "", // Empty token
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(map[string]pelecard.MuhlafimEntry{}, nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.Equal(t, 0, result.Processed) // Order with empty token is skipped
}

func TestProcessMuhlafim_MissingToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	orders := []repo.Order{{ID: 1}}
	tokenMap := map[int]string{} // Token not in map

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, []int{1}).Return(tokenMap, nil)
	mockPelecard.EXPECT().FetchMuhlafim(ctx, mock.Anything, mock.Anything).Return(map[string]pelecard.MuhlafimEntry{}, nil)

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, false)

	assert.NoError(t, err)
	assert.Equal(t, 0, result.Processed) // Order without token is skipped
}

func TestProcessMuhlafim_DryRunSkipsPelecardAPI(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockPelecard := pelecardmock.NewMockPelecardAPI(t)

	// Build enough orders so the 0.5% simulation produces at least one match.
	// With the Knuth hash, orderID * 2654435761 % 1000 < 5 gives a match.
	// We generate 1000 orders and expect ~5 matches.
	orders := make([]repo.Order, 1000)
	orderIDs := make([]int, 1000)
	tokenMap := make(map[int]string, 1000)
	for i := range orders {
		id := i + 1
		orders[i] = repo.Order{ID: id}
		orderIDs[i] = id
		tokenMap[id] = fmt.Sprintf("token_%d", id)
	}

	mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
	mockRepo.EXPECT().GetTokensForOrders(ctx, orderIDs).Return(tokenMap, nil)
	// FetchMuhlafim must NOT be called in dry-run mode
	mockRepo.EXPECT().FlagOrder(ctx, mock.AnythingOfType("int"), mock.AnythingOfType("string")).Return(nil).Maybe()

	result, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, true)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	// With 1000 orders at 0.5%, expect some matches
	assert.Greater(t, result.Processed, 0, "dry-run should produce some muhlafim matches")
	// Verify we got both new cards and flag updates
	totalActions := result.Updated + result.NewCards
	assert.Equal(t, result.Processed, totalActions, "each processed order should be either new card or flag update")
}

func TestProcessMuhlafim_DryRunDeterministic(t *testing.T) {
	// Run the same dry-run twice and verify identical results
	ctx := context.Background()

	orders := make([]repo.Order, 500)
	orderIDs := make([]int, 500)
	tokenMap := make(map[int]string, 500)
	for i := range orders {
		id := i + 1
		orders[i] = repo.Order{ID: id}
		orderIDs[i] = id
		tokenMap[id] = fmt.Sprintf("token_%d", id)
	}

	var results [2]*ProcessMuhlafimResult
	for run := 0; run < 2; run++ {
		mockRepo := mocks.NewMockOrdersRepository(t)
		mockPelecard := pelecardmock.NewMockPelecardAPI(t)

		mockRepo.EXPECT().GetFlaggedOrders(ctx).Return(orders, nil)
		mockRepo.EXPECT().GetTokensForOrders(ctx, orderIDs).Return(tokenMap, nil)
		mockRepo.EXPECT().FlagOrder(ctx, mock.AnythingOfType("int"), mock.AnythingOfType("string")).Return(nil).Maybe()

		r, err := ProcessMuhlafim(ctx, mockRepo, mockPelecard, time.Now(), time.Now(), false, true)
		assert.NoError(t, err)
		results[run] = r
	}

	assert.Equal(t, results[0].Processed, results[1].Processed)
	assert.Equal(t, results[0].Updated, results[1].Updated)
	assert.Equal(t, results[0].NewCards, results[1].NewCards)
	assert.Equal(t, results[0].Flags, results[1].Flags)
}

func TestFormatPelecardDate(t *testing.T) {
	testTime := time.Date(2024, time.June, 15, 14, 30, 0, 0, time.UTC)
	formatted := formatPelecardDate(testTime)

	expected := "15/06/2024 14:30"
	assert.Equal(t, expected, formatted)
}

func TestMaskCardNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Normal card", "1234567890123456", "****3456"},
		{"Short card", "1234", "****"},
		{"Very short", "12", "****"},
		{"Empty", "", "****"},
		{"Exactly 4 digits", "1234", "****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskCardNumber(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
