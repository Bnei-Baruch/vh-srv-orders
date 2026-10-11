package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// SkipDoubleOrders skips orders for users who had double orders in the past month.
// It identifies accounts with multiple paid/cancelled orders in the specified month
// and sets their flagged orders to 'skip'.
func SkipDoubleOrders(ctx context.Context, ordersRepo repo.OrdersRepository, year, month int, lastDay time.Time) (int, error) {
	utils.LogFor(ctx).Info("Skipping customers with double orders from last month",
		slog.Int("year", year),
		slog.Int("month", month))

	accountIDs, err := ordersRepo.GetOrdersToSkipDouble(ctx, year, month, lastDay)
	if err != nil {
		return 0, fmt.Errorf("get orders to skip double: %w", err)
	}

	totalSkipped := 0
	for _, accountID := range accountIDs {
		count, err := ordersRepo.SkipOrdersByAccountID(ctx, accountID)
		if err != nil {
			utils.LogFor(ctx).Warn("Failed to skip orders for account",
				slog.Int("account_id", accountID),
				slog.Any("error", err))
			continue
		}
		totalSkipped += count
		utils.LogFor(ctx).Info("Skipped orders for account",
			slog.Int("account_id", accountID),
			slog.Int("count", count))
	}

	utils.LogFor(ctx).Info("Total orders skipped for double payments",
		slog.Int("total", totalSkipped))
	return totalSkipped, nil
}

// SkipFreshOrders skips orders for users who already paid this month.
// It identifies accounts that have paid or cancelled orders in the current billing period
// and sets their flagged orders to 'skip'.
func SkipFreshOrders(ctx context.Context, ordersRepo repo.OrdersRepository, year, month int, lastDay time.Time) (int, error) {
	utils.LogFor(ctx).Info("Skipping customers who already paid this month",
		slog.Int("year", year),
		slog.Int("month", month))

	accountIDs, err := ordersRepo.GetOrdersToSkipFresh(ctx, year, month, lastDay)
	if err != nil {
		return 0, fmt.Errorf("get orders to skip fresh: %w", err)
	}

	totalSkipped := 0
	for _, accountID := range accountIDs {
		count, err := ordersRepo.SkipOrdersByAccountID(ctx, accountID)
		if err != nil {
			utils.LogFor(ctx).Warn("Failed to skip orders for account",
				slog.Int("account_id", accountID),
				slog.Any("error", err))
			continue
		}
		totalSkipped += count
		utils.LogFor(ctx).Info("Skipped orders for account",
			slog.Int("account_id", accountID),
			slog.Int("count", count))
	}

	utils.LogFor(ctx).Info("Total orders skipped for fresh payments",
		slog.Int("total", totalSkipped))
	return totalSkipped, nil
}
