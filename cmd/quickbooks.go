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

var quickbooksCmd = &cobra.Command{
	Use:   "quickbooks",
	Short: "QuickBooks commands (via vh-srv-accounting)",
	Long:  "Commands for interacting with the vh-srv-accounting service (QuickBooks integration)",
}

var qbLastContributionsCmd = &cobra.Command{
	Use:   "last-contributions [email]",
	Short: "Get last contributions by email",
	Long:  "Get the last 12 months of contributions (summed by currency) for a customer from QuickBooks via vh-srv-accounting",
	Args:  cobra.ExactArgs(1),
	Run:   qbLastContributionsFn,
}

func init() {
	rootCmd.AddCommand(quickbooksCmd)

	qbLastContributionsCmd.Flags().String("company-id", "", "QuickBooks company (realm) ID; omit to aggregate across all enabled companies")
	quickbooksCmd.AddCommand(qbLastContributionsCmd)
}

func qbLastContributionsFn(cmd *cobra.Command, args []string) {
	email := args[0]

	if common.Config.AccountingServiceUrl == "" {
		slog.Error("ACCOUNTING_SERVICE_URL environment variable is required")
		os.Exit(1)
	}

	companyIDFlag, err := cmd.Flags().GetString("company-id")
	if err != nil {
		slog.Error("Failed to read company-id flag", slog.Any("error", err))
		os.Exit(1)
	}
	var companyID *string
	companyIDLog := "all"
	if companyIDFlag != "" {
		companyID = &companyIDFlag
		companyIDLog = companyIDFlag
	}

	client := accounting.NewAccountingServiceAPI(keycloak.NewClient())
	ctx := context.Background()

	slog.Info("Fetching last contributions from vh-srv-accounting",
		slog.String("email", email),
		slog.String("company_id", companyIDLog))

	result, err := client.GetLastContributions(ctx, email, companyID)
	if err != nil {
		slog.Error("Failed to fetch last contributions", slog.Any("error", err))
		os.Exit(1)
	}

	fmt.Printf("\nLast 12 months contributions for email: %s\n", email)
	fmt.Println(strings.Repeat("=", 82))

	if !result.Found {
		fmt.Println("\nUser not found in any enabled QuickBooks company.")
		fmt.Println("\n" + strings.Repeat("=", 82))
		return
	}

	fmt.Println("\nTotal:")
	if len(result.Total) == 0 {
		fmt.Println("  (no contributions)")
	} else {
		for currency, amount := range result.Total {
			fmt.Printf("  %s: %.2f\n", currency, amount)
		}
	}

	for _, company := range result.Companies {
		fmt.Printf("\nCompany: %s (%s) — found: %t\n", company.CompanyName, company.CompanyID, company.Found)
		if len(company.Contributions) == 0 {
			fmt.Println("  (no contributions)")
			continue
		}
		for currency, amount := range company.Contributions {
			fmt.Printf("  %s: %.2f\n", currency, amount)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 82))
}
