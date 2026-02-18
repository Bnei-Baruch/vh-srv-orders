package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ProcessMuhlafimResult contains statistics about muhlafim processing
type ProcessMuhlafimResult struct {
	Processed int
	Updated   int
	NewCards  int
	Flags     map[string]int
}

// ProcessMuhlafim processes muhlafim data from Pelecard and updates orders accordingly.
// It fetches flagged orders, gets their tokens, fetches muhlafim data from Pelecard,
// and processes each order to either incorporate new cards or set appropriate flags.
// If saveToFile is true, it writes the muhlafim data to muhlafim.json file.
// When dryRun is true, muhlafim data is simulated instead of fetched from Pelecard
// (0.5% match rate, 80% new cards, 20% random muh_ flags).
func ProcessMuhlafim(ctx context.Context, ordersRepo repo.OrdersRepository, pelecardClient pelecard.PelecardAPI, startDate, endDate time.Time, saveToFile bool, dryRun bool) (*ProcessMuhlafimResult, error) {
	// Fetch flagged orders and their tokens
	orders, tokenMap, err := fetchFlaggedOrdersWithTokens(ctx, ordersRepo)
	if err != nil {
		return nil, fmt.Errorf("fetch flagged orders with tokens: %w", err)
	}

	if len(orders) == 0 {
		utils.LogFor(ctx).Info("No flagged orders found for muhlafim processing")
		return &ProcessMuhlafimResult{}, nil
	}

	// Fetch or simulate muhlafim data
	var muhlafimData map[string]pelecard.MuhlafimEntry
	if dryRun {
		utils.LogFor(ctx).Info("Dry-run: simulating muhlafim data (0.5%% match, 80%% new card, 20%% muh_ flag)")
		muhlafimData = generateDryRunMuhlafim(tokenMap)
		utils.LogFor(ctx).Info("Dry-run: generated simulated muhlafim entries", slog.Int("count", len(muhlafimData)))
	} else {
		startDateStr := formatPelecardDate(startDate)
		endDateStr := formatPelecardDate(endDate)
		muhlafimData, err = fetchMuhlafimData(ctx, pelecardClient, startDateStr, endDateStr)
		if err != nil {
			return nil, fmt.Errorf("fetch muhlafim data: %w", err)
		}
	}

	// Save to file if requested (for standalone command)
	if saveToFile {
		if err := saveMuhlafimToFile(muhlafimData); err != nil {
			// Log warning but don't fail the operation
			utils.LogFor(ctx).Warn("Failed to save muhlafim data to file", slog.Any("error", err))
		}
	}

	// Process each order
	result := &ProcessMuhlafimResult{
		Flags: make(map[string]int),
	}
	for _, order := range orders {
		token, exists := tokenMap[order.ID]
		if !exists || token == "" {
			utils.LogFor(ctx).Warn("No token found for order", slog.Int("order_id", order.ID))
			continue
		}

		muhlafimEntry, found := muhlafimData[token]
		if !found {
			continue
		}

		stats, err := processOrderWithMuhlafim(ctx, ordersRepo, order, muhlafimEntry)
		if err != nil {
			utils.LogFor(ctx).Error("Failed to process order with muhlafim",
				slog.Int("order_id", order.ID),
				slog.Any("error", err))
			continue
		}

		if stats.processed {
			result.Processed++
		}
		if stats.flag != "" {
			result.Flags[stats.flag]++
			result.Updated++
		}
		if stats.newCard {
			result.NewCards++
		}
	}

	return result, nil
}

// fetchFlaggedOrdersWithTokens fetches all flagged orders and their corresponding tokens.
// Returns orders, a map of orderID -> token, and any error.
func fetchFlaggedOrdersWithTokens(ctx context.Context, ordersRepo repo.OrdersRepository) ([]repo.Order, map[int]string, error) {
	utils.LogFor(ctx).Info("Fetching flagged orders")
	orders, err := ordersRepo.GetFlaggedOrders(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get flagged orders: %w", err)
	}

	if len(orders) == 0 {
		return orders, nil, nil
	}

	utils.LogFor(ctx).Info("Found flagged orders", slog.Int("count", len(orders)))

	// Batch fetch tokens for all orders (optimized to avoid N+1 queries)
	orderIDs := make([]int, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.ID
	}

	utils.LogFor(ctx).Info("Fetching tokens for orders")
	tokenMap, err := ordersRepo.GetTokensForOrders(ctx, orderIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("get tokens for orders: %w", err)
	}

	return orders, tokenMap, nil
}

// fetchMuhlafimData fetches muhlafim data from Pelecard API.
// startDate and endDate should be in Pelecard's format "DD/MM/YYYY HH:MM".
func fetchMuhlafimData(ctx context.Context, pelecardClient pelecard.PelecardAPI, startDate, endDate string) (map[string]pelecard.MuhlafimEntry, error) {
	utils.LogFor(ctx).Info("Fetching muhlafim data", slog.String("start-date", startDate), slog.String("end-date", endDate))
	muhlafimData, err := pelecardClient.FetchMuhlafim(ctx, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("pelecard client fetch muhlafim: %w", err)
	}

	utils.LogFor(ctx).Info("Fetched muhlafim entries", slog.Int("count", len(muhlafimData)))
	return muhlafimData, nil
}

// saveMuhlafimToFile writes muhlafim data to muhlafim.json file.
func saveMuhlafimToFile(muhlafimData map[string]pelecard.MuhlafimEntry) error {
	muhlafimJSON, err := json.MarshalIndent(muhlafimData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal muhlafim data: %w", err)
	}

	if err := os.WriteFile("muhlafim.json", muhlafimJSON, 0600); err != nil {
		return fmt.Errorf("write muhlafim.json file: %w", err)
	}

	return nil
}

// processOrderStats tracks the processing statistics for a single order
type processOrderStats struct {
	processed bool
	newCard   bool
	flag      string
}

// processOrderWithMuhlafim processes a single order with its muhlafim entry.
// It either incorporates a new card (if NewCardNumber is present) or sets an appropriate flag.
func processOrderWithMuhlafim(ctx context.Context, ordersRepo repo.OrdersRepository, order repo.Order, muhlafimEntry pelecard.MuhlafimEntry) (*processOrderStats, error) {
	stats := &processOrderStats{}

	utils.LogFor(ctx).Info("Processing order",
		slog.Int("order_id", order.ID),
		slog.String("action", muhlafimEntry.ActionDescription),
		slog.String("has_new_card", fmt.Sprintf("%t", len(muhlafimEntry.NewCardNumber) > 0)))

	stats.processed = true

	// If there's a new card number, mark it but don't update (order remains flagged for renewal)
	if len(muhlafimEntry.NewCardNumber) > 0 {
		utils.LogFor(ctx).Info("New card detected for order - order will remain flagged for renewal",
			slog.Int("order_id", order.ID),
			slog.String("masked_card", maskCardNumber(muhlafimEntry.NewCardNumber)))
		stats.newCard = true
		return stats, nil
	}

	// Otherwise, determine flag based on action description
	switch muhlafimEntry.ActionDescription {
	case pelecard.MUH_HIYUV_NIKLAT:
		stats.flag = common.OrderFlagMuhHiyuvNiklat
	case pelecard.MUH_NIDHA:
		stats.flag = common.OrderFlagMuhNidha
	case pelecard.MUH_BITUL:
		stats.flag = common.OrderFlagMuhBitul
	case pelecard.MUH_LOTAKIN:
		stats.flag = common.OrderFlagMuhLotakin
	default:
		stats.flag = common.OrderFlagMuhAher
	}

	// Update order flag
	if err := ordersRepo.FlagOrder(ctx, order.ID, stats.flag); err != nil {
		return stats, fmt.Errorf("flag order: %w", err)
	}

	utils.LogFor(ctx).Info("Updated order flag", slog.Int("order_id", order.ID), slog.String("flag", stats.flag))
	return stats, nil
}

// maskCardNumber masks a card number for logging purposes (shows only last 4 digits)
func maskCardNumber(cardNumber string) string {
	if len(cardNumber) <= 4 {
		return "****"
	}
	return "****" + cardNumber[len(cardNumber)-4:]
}

// formatPelecardDate converts a time.Time to Pelecard's date format "DD/MM/YYYY HH:MM"
func formatPelecardDate(t time.Time) string {
	return t.Format("02/01/2006 15:04")
}
