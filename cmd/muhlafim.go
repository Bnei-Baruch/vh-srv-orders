package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/billing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// Still spelled "pelecard" so existing runbooks keep working, though this
// service no longer reaches Pelecard: external_payments does, and owns the
// credentials and terminals.
var pelecardCmd = &cobra.Command{
	Use:   "pelecard",
	Short: "Payment commands, served by external_payments",
	Long:  "Payment commands. See subcommands for more details.",
}

var muhlafimCmd = &cobra.Command{
	Use:   "muhlafim",
	Short: "Process muhlafim (card status updates)",
	Long:  "Fetches muhlafim data from external_payments and updates order flags based on action descriptions",
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
	startDateStr, endDateStr := parseFlags(cmd)
	validateConfig()

	ctx := context.Background()
	eventEmitter, ordersDB := initializeServices(ctx)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eventEmitter.Close(ctx)
	}()
	defer ordersDB.Close()

	// Parse date strings to time.Time
	startDate, err := parsePelecardDate(startDateStr)
	if err != nil {
		utils.LogFatal("Failed to parse start-date", slog.String("date", startDateStr), slog.Any("error", err))
	}

	endDate, err := parsePelecardDate(endDateStr)
	if err != nil {
		utils.LogFatal("Failed to parse end-date", slog.String("date", endDateStr), slog.Any("error", err))
	}

	// Initialize Pelecard client
	pelecardClient := pelecard.NewClient()

	// Process muhlafim using domain logic
	result, err := billing.ProcessMuhlafim(ctx, ordersDB, pelecardClient, startDate, endDate, true, false)
	if err != nil {
		utils.LogFatal("Failed to process muhlafim", slog.Any("error", err))
	}

	utils.LogFor(ctx).Info("Processing complete",
		slog.Int("processed", result.Processed),
		slog.Int("updated", result.Updated),
		slog.Int("new_cards", result.NewCards))
}

func parseFlags(cmd *cobra.Command) (string, string) {
	startDate, err := cmd.Flags().GetString("start-date")
	if err != nil {
		utils.LogFatal("Failed to read start-date flag", slog.Any("error", err))
	}

	endDate, err := cmd.Flags().GetString("end-date")
	if err != nil {
		utils.LogFatal("Failed to read end-date flag", slog.Any("error", err))
	}

	return startDate, endDate
}

func validateConfig() {
	// Muhlafim comes from external_payments now, so what this needs is a
	// Keycloak identity rather than Pelecard credentials.
	if common.Config.KeycloakServerUrl == "" || common.Config.KeycloakRealm == "" {
		utils.LogFatal("KEYCLOAK_SERVER_URL and KEYCLOAK_REALM are required")
	}
	if common.Config.KeycloakClientID == "" || common.Config.KeycloakClientSecret == "" {
		utils.LogFatal("KEYCLOAK_CLIENT_ID and KEYCLOAK_CLIENT_SECRET are required")
	}
}

func initializeServices(ctx context.Context) (events.EventEmitter, *repo.OrdersDB) {
	eventEmitter, err := events.CreateEmitter()
	if err != nil {
		utils.LogFatal("Failed to create event emitter", slog.Any("error", err))
	}

	ordersDB, err := repo.NewOrdersDB(ctx, eventEmitter)
	if err != nil {
		utils.LogFatal("Failed to initialize database", slog.Any("error", err))
	}

	return eventEmitter, ordersDB
}

// parsePelecardDate parses a date string in Pelecard's format "DD/MM/YYYY HH:MM" to time.Time
func parsePelecardDate(dateStr string) (time.Time, error) {
	layout := "02/01/2006 15:04"
	return time.Parse(layout, dateStr)
}
