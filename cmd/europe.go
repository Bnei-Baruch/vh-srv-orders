package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
)

var europeCmd = &cobra.Command{
	Use:   "europe",
	Short: "European donations commands (via vh-srv-accounting)",
	Long:  "Commands for interacting with the vh-srv-accounting service (European donations integration)",
}

var europeContributionsCmd = &cobra.Command{
	Use:   "contributions",
	Short: "Get last contributions by email(s)",
	Long:  "Get the last 12 months of contributions (summed by currency) per email from the European donations system via vh-srv-accounting",
	Run:   europeContributionsFn,
}

func init() {
	rootCmd.AddCommand(europeCmd)

	europeContributionsCmd.Flags().StringSlice("email", nil, "email to look up (repeatable)")
	_ = europeContributionsCmd.MarkFlagRequired("email")
	europeCmd.AddCommand(europeContributionsCmd)
}

func europeContributionsFn(cmd *cobra.Command, _ []string) {
	if common.Config.AccountingServiceUrl == "" {
		slog.Error("ACCOUNTING_SERVICE_URL environment variable is required")
		os.Exit(1)
	}

	emails, err := cmd.Flags().GetStringSlice("email")
	if err != nil {
		slog.Error("Failed to read email flag", slog.Any("error", err))
		os.Exit(1)
	}

	client := accounting.NewAccountingServiceAPI(keycloak.NewClient())
	ctx := context.Background()

	slog.Info("Fetching Europe contributions from vh-srv-accounting",
		slog.String("emails", strings.Join(emails, ", ")))

	result, err := client.GetEuropeContributions(ctx, emails)
	if err != nil {
		slog.Error("Failed to fetch Europe contributions", slog.Any("error", err))
		os.Exit(1)
	}

	fmt.Printf("\nEurope contributions (lookback: %d months, cutoff: %s)\n", result.LookbackMonths, result.CutoffDate)
	fmt.Println(strings.Repeat("=", 82))

	for _, entry := range result.Results {
		fmt.Printf("\n%s (%s) — found: %t\n", entry.Identifier, entry.IdentifierType, entry.Found)
		if !entry.Found || len(entry.Contributions) == 0 {
			fmt.Println("  (no contributions)")
			continue
		}
		for currency, amount := range entry.Contributions {
			fmt.Printf("  %s: %.2f\n", currency, amount)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 82))
}
