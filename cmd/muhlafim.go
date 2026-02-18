package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

var pelecardCmd = &cobra.Command{
	Use:   "pelecard",
	Short: "Pelecard commands",
	Long:  "Pelecard commands. See subcommands for more details.",
}

var muhlafimCmd = &cobra.Command{
	Use:   "muhlafim",
	Short: "Process muhlafim (card status updates) from Pelecard",
	Long:  "Fetches muhlafim data from Pelecard API and updates order flags based on action descriptions",
	Run:   muhlafimFn,
}

func init() {
	rootCmd.AddCommand(pelecardCmd)
	pelecardCmd.AddCommand(muhlafimCmd)

	muhlafimCmd.Flags().String("start-date", "", "Start date in format DD/MM/YYYY HH:MM (e.g., 21/08/2025 00:00)")
	muhlafimCmd.Flags().String("end-date", "", "End date in format DD/MM/YYYY HH:MM (e.g., 24/09/2025 00:00)")
	muhlafimCmd.MarkFlagRequired("start-date")
	muhlafimCmd.MarkFlagRequired("end-date")
}

func muhlafimFn(cmd *cobra.Command, args []string) {
	startDate, endDate := parseFlags(cmd)
	validateConfig()

	ctx := context.Background()
	eventEmitter, ordersDB := initializeServices(ctx)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eventEmitter.Close(ctx)
	}()
	defer ordersDB.Close()

	orders, tokenMap := fetchOrderData(ctx, ordersDB)
	if len(orders) == 0 {
		slog.Info("No flagged orders found")
		return
	}

	muhlafimData := fetchAndSaveMuhlafimData(ctx, startDate, endDate)

	// Process each order
	processedCount := 0
	updatedCount := 0
	newCardCount := 0
	for _, order := range orders {
		token, exists := tokenMap[order.ID]
		if !exists || token == "" {
			slog.Warn("No token found for order", slog.Int("order_id", order.ID))
			continue
		}

		muhlafimEntry, found := muhlafimData[token]
		if !found {
			continue
		}

		stats := processOrder(ctx, ordersDB, order, muhlafimEntry)
		if stats.processed {
			processedCount++
		}
		if stats.updated {
			updatedCount++
		}
		if stats.newCard {
			newCardCount++
		}
	}

	slog.Info("Processing complete",
		slog.Int("total_orders", len(orders)),
		slog.Int("processed", processedCount),
		slog.Int("updated", updatedCount),
		slog.Int("new_cards", newCardCount))
}

type processOrderStats struct {
	processed bool
	updated   bool
	newCard   bool
}

func parseFlags(cmd *cobra.Command) (string, string) {
	startDate, err := cmd.Flags().GetString("start-date")
	if err != nil {
		slog.Error("Failed to read start-date flag", slog.Any("error", err))
		os.Exit(1)
	}

	endDate, err := cmd.Flags().GetString("end-date")
	if err != nil {
		slog.Error("Failed to read end-date flag", slog.Any("error", err))
		os.Exit(1)
	}

	return startDate, endDate
}

func validateConfig() {
	if common.Config.PelecardNewTerminalNumber == "" {
		slog.Error("PELECARD_NEW_TERMINAL_NUMBER environment variable is required")
		os.Exit(1)
	}
	if common.Config.PelecardUser == "" {
		slog.Error("PELECARD_USER environment variable is required")
		os.Exit(1)
	}
	if common.Config.PelecardPassword == "" {
		slog.Error("PELECARD_PASSWORD environment variable is required")
		os.Exit(1)
	}
}

func initializeServices(ctx context.Context) (events.EventEmitter, *repo.OrdersDB) {
	eventEmitter, err := events.CreateEmitter()
	if err != nil {
		slog.Error("Failed to create event emitter", slog.Any("error", err))
		os.Exit(1)
	}

	ordersDB, err := repo.NewOrdersDB(ctx, eventEmitter)
	if err != nil {
		slog.Error("Failed to initialize database", slog.Any("error", err))
		os.Exit(1)
	}

	return eventEmitter, ordersDB
}

func fetchOrderData(ctx context.Context, ordersDB *repo.OrdersDB) ([]repo.Order, map[int]string) {
	slog.Info("Fetching flagged orders")
	orders, err := ordersDB.GetFlaggedOrders(ctx)
	if err != nil {
		slog.Error("Failed to fetch flagged orders", slog.Any("error", err))
		os.Exit(1)
	}

	if len(orders) == 0 {
		return orders, nil
	}

	slog.Info("Found flagged orders", slog.Int("count", len(orders)))

	// Batch fetch tokens for all orders (optimized to avoid N+1 queries)
	orderIDs := make([]int, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.ID
	}

	slog.Info("Fetching tokens for orders")
	tokenMap, err := ordersDB.GetTokensForOrders(ctx, orderIDs)
	if err != nil {
		slog.Error("Failed to fetch tokens", slog.Any("error", err))
		os.Exit(1)
	}

	return orders, tokenMap
}

func fetchAndSaveMuhlafimData(ctx context.Context, startDate, endDate string) map[string]pelecard.MuhlafimEntry {
	slog.Info("Fetching muhlafim data", slog.String("start-date", startDate), slog.String("end-date", endDate))
	pelecardClient := pelecard.NewClient()
	muhlafimData, err := pelecardClient.FetchMuhlafim(ctx, startDate, endDate)
	if err != nil {
		slog.Error("Failed to fetch muhlafim data", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("Fetched muhlafim entries", slog.Int("count", len(muhlafimData)))

	// Write muhlafim.json file
	muhlafimJSON, err := json.MarshalIndent(muhlafimData, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal muhlafim data", slog.Any("error", err))
		os.Exit(1)
	}

	if err := os.WriteFile("muhlafim.json", muhlafimJSON, 0644); err != nil {
		slog.Error("Failed to write muhlafim.json", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("Wrote muhlafim.json file")

	return muhlafimData
}

func processOrder(ctx context.Context, ordersDB *repo.OrdersDB, order repo.Order, muhlafimEntry pelecard.MuhlafimEntry) processOrderStats {
	stats := processOrderStats{}

	slog.Info("Processing order",
		slog.Int("order_id", order.ID),
		slog.String("action", muhlafimEntry.ActionDescription),
		slog.String("has_new_card", fmt.Sprintf("%t", len(muhlafimEntry.NewCardNumber) > 0)))

	stats.processed = true

	// Determine flag based on action description and NewCardNumber
	var flag string
	shouldUpdate := false

	switch muhlafimEntry.ActionDescription {
	case pelecard.MUH_HIYUV_NIKLAT:
		if len(muhlafimEntry.NewCardNumber) > 0 {
			slog.Info("NEW CARD", slog.Int("order_id", order.ID))
			stats.newCard = true
		} else {
			flag = common.OrderFlagMuhHiyuvNiklat
			shouldUpdate = true
		}
	case pelecard.MUH_NIDHA:
		if len(muhlafimEntry.NewCardNumber) > 0 {
			slog.Info("NEW CARD", slog.Int("order_id", order.ID))
			stats.newCard = true
		} else {
			flag = common.OrderFlagMuhNidha
			shouldUpdate = true
		}
	case pelecard.MUH_BITUL:
		if len(muhlafimEntry.NewCardNumber) > 0 {
			slog.Info("NEW CARD", slog.Int("order_id", order.ID))
			stats.newCard = true
		} else {
			flag = common.OrderFlagMuhBitul
			shouldUpdate = true
		}
	case pelecard.MUH_LOTAKIN:
		if len(muhlafimEntry.NewCardNumber) > 0 {
			slog.Info("NEW CARD", slog.Int("order_id", order.ID))
			stats.newCard = true
		} else {
			flag = common.OrderFlagMuhLotakin
			shouldUpdate = true
		}
	default:
		flag = common.OrderFlagMuhAher
		shouldUpdate = true
	}

	if shouldUpdate {
		if err := ordersDB.FlagOrder(ctx, order.ID, flag); err != nil {
			slog.Error("Failed to update order flag",
				slog.Int("order_id", order.ID),
				slog.String("flag", flag),
				slog.Any("error", err))
			return stats
		}
		stats.updated = true
		slog.Info("Updated order flag", slog.Int("order_id", order.ID), slog.String("flag", flag))
	}

	return stats
}
