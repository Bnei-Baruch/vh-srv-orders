package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
)

func TestSkipDoubleOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 5
	lastDay := time.Date(2024, time.May, 31, 23, 59, 59, 0, time.UTC)

	userkeys := []string{"user1", "user2", "user3"}

	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, year, month, lastDay).Return(userkeys, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user1").Return(2, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user2").Return(1, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user3").Return(3, nil)

	totalSkipped, err := SkipDoubleOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 6, totalSkipped) // 2 + 1 + 3
}

func TestSkipDoubleOrders_NoUserkeys(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 5
	lastDay := time.Date(2024, time.May, 31, 23, 59, 59, 0, time.UTC)

	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, year, month, lastDay).Return([]string{}, nil)

	totalSkipped, err := SkipDoubleOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 0, totalSkipped)
}

func TestSkipDoubleOrders_ErrorGettingUserkeys(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 5
	lastDay := time.Date(2024, time.May, 31, 23, 59, 59, 0, time.UTC)

	expectedErr := errors.New("database error")
	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, year, month, lastDay).Return(nil, expectedErr)

	totalSkipped, err := SkipDoubleOrders(ctx, mockRepo, year, month, lastDay)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get orders to skip double")
	assert.Equal(t, 0, totalSkipped)
}

func TestSkipDoubleOrders_PartialFailure(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 5
	lastDay := time.Date(2024, time.May, 31, 23, 59, 59, 0, time.UTC)

	userkeys := []string{"user1", "user2", "user3"}

	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, year, month, lastDay).Return(userkeys, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user1").Return(2, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user2").Return(0, errors.New("skip error"))
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user3").Return(3, nil)

	// Should continue processing even if one fails
	totalSkipped, err := SkipDoubleOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 5, totalSkipped) // 2 + 0 + 3 (user2 failed but we continue)
}

func TestSkipFreshOrders(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 6
	lastDay := time.Date(2024, time.June, 30, 23, 59, 59, 0, time.UTC)

	userkeys := []string{"user1", "user2"}

	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, year, month, lastDay).Return(userkeys, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user1").Return(1, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user2").Return(2, nil)

	totalSkipped, err := SkipFreshOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 3, totalSkipped) // 1 + 2
}

func TestSkipFreshOrders_NoUserkeys(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 6
	lastDay := time.Date(2024, time.June, 30, 23, 59, 59, 0, time.UTC)

	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, year, month, lastDay).Return([]string{}, nil)

	totalSkipped, err := SkipFreshOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 0, totalSkipped)
}

func TestSkipFreshOrders_ErrorGettingUserkeys(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 6
	lastDay := time.Date(2024, time.June, 30, 23, 59, 59, 0, time.UTC)

	expectedErr := errors.New("database error")
	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, year, month, lastDay).Return(nil, expectedErr)

	totalSkipped, err := SkipFreshOrders(ctx, mockRepo, year, month, lastDay)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get orders to skip fresh")
	assert.Equal(t, 0, totalSkipped)
}

func TestSkipFreshOrders_PartialFailure(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 6
	lastDay := time.Date(2024, time.June, 30, 23, 59, 59, 0, time.UTC)

	userkeys := []string{"user1", "user2"}

	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, year, month, lastDay).Return(userkeys, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user1").Return(0, errors.New("skip error"))
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user2").Return(2, nil)

	// Should continue processing even if one fails
	totalSkipped, err := SkipFreshOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 2, totalSkipped) // 0 + 2 (user1 failed but we continue)
}

func TestSkipDoubleOrders_EmptyUserkey(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 5
	lastDay := time.Date(2024, time.May, 31, 23, 59, 59, 0, time.UTC)

	userkeys := []string{"user1", "", "user2"}

	mockRepo.EXPECT().GetOrdersToSkipDouble(ctx, year, month, lastDay).Return(userkeys, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user1").Return(1, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "").Return(0, nil)
	mockRepo.EXPECT().SkipOrdersByUserKey(ctx, "user2").Return(2, nil)

	totalSkipped, err := SkipDoubleOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 3, totalSkipped) // 1 + 0 + 2
}

func TestSkipFreshOrders_MultipleUserkeys(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)

	year := 2024
	month := 6
	lastDay := time.Date(2024, time.June, 30, 23, 59, 59, 0, time.UTC)

	userkeys := []string{"user1", "user2", "user3", "user4", "user5"}

	mockRepo.EXPECT().GetOrdersToSkipFresh(ctx, year, month, lastDay).Return(userkeys, nil)
	for i, userkey := range userkeys {
		mockRepo.EXPECT().SkipOrdersByUserKey(ctx, userkey).Return(i+1, nil)
	}

	totalSkipped, err := SkipFreshOrders(ctx, mockRepo, year, month, lastDay)

	assert.NoError(t, err)
	assert.Equal(t, 15, totalSkipped) // 1 + 2 + 3 + 4 + 5
}
