package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
)

var priorityCmd = &cobra.Command{
	Use:   "priority",
	Short: "Priority ERP commands",
	Long:  "Commands for interacting with Priority ERP system",
}

// Command: get-customer nested under priorityCmd
var getCustomerCmd = &cobra.Command{
	Use:   "get-customer",
	Short: "Get customer details by email from Priority ERP",
	Run:   getCustomerFn,
}

var accountReceivablesCmd = &cobra.Command{
	Use:   "account-receivables [email]",
	Short: "Fetch account receivables for a customer",
	Long:  "Fetch all account receivables associated with the given customer from Priority ERP",
	Args:  cobra.ExactArgs(1),
	Run:   accountReceivablesFn,
}

var getCustomerByIDCmd = &cobra.Command{
	Use:   "get-customer-by-id [customerID]",
	Short: "Get customer details by Priority customer ID (CUSTNAME)",
	Long:  "Fetch a single customer from Priority ERP by CUSTNAME (customer code)",
	Args:  cobra.ExactArgs(1),
	Run:   getCustomerByIDFn,
}

var accountReceivablesByIDCmd = &cobra.Command{
	Use:   "account-receivables-by-id [customerID]",
	Short: "Fetch account receivables by Priority customer ID (CUSTNAME)",
	Long:  "Fetch all account receivables for the given Priority customer ID (CUSTNAME) directly, without an email lookup",
	Args:  cobra.ExactArgs(1),
	Run:   accountReceivablesByIDFn,
}

var lastContributionsCmd = &cobra.Command{
	Use:   "last-contributions [emails...]",
	Short: "Get last contributions for one or more emails",
	Long:  "Get the last 12 months of contributions (summed by currency) from Priority ERP for one or more emails, treated as one group (e.g. aliases of the same person) so a customer matched by more than one email is only counted once. Uses chunked filtered/selected OData requests, so request count and bytes transferred stay flat as the number of emails grows. Emails may be given as separate args and/or comma-separated. Prints request/byte/duration stats.",
	Args:  cobra.MinimumNArgs(1),
	Run:   lastContributionsFn,
}

func init() {
	rootCmd.AddCommand(priorityCmd)

	getCustomerCmd.Flags().String("email", "", "Email address to query")
	priorityCmd.AddCommand(getCustomerCmd)

	priorityCmd.AddCommand(accountReceivablesCmd)
	priorityCmd.AddCommand(getCustomerByIDCmd)
	priorityCmd.AddCommand(accountReceivablesByIDCmd)
	priorityCmd.AddCommand(lastContributionsCmd)
}

func accountReceivablesFn(cmd *cobra.Command, args []string) {
	email := args[0]

	// Validate configuration
	if common.Config.PriorityBaseURL == "" {
		slog.Error("PRIORITY_BASE_URL environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityUsername == "" {
		slog.Error("PRIORITY_USERNAME environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityPassword == "" {
		slog.Error("PRIORITY_PASSWORD environment variable is required")
		os.Exit(1)
	}

	client := priority.NewClient()
	ctx := context.Background()

	slog.Info("Fetching account receivables from Priority ERP", slog.String("email", email))

	customers, err := client.GetActiveCustomersByEmail(ctx, email)
	if err != nil {
		slog.Error("Failed to fetch customers", slog.Any("error", err))
		os.Exit(1)
	}
	if len(customers) == 0 {
		fmt.Printf("\nNo active customers found for email: %s\n", email)
		os.Exit(0)
	}

	totalItems := 0
	for _, customer := range customers {
		accountReceivables, err := client.GetAccountReceivables(ctx, customer.CustName)
		if err != nil {
			slog.Error("Failed to fetch account receivables", slog.String("custName", customer.CustName), slog.Any("error", err))
			os.Exit(1)
		}

		if len(accountReceivables) == 0 {
			fmt.Printf("\nNo account receivables found for customer: %s (%s)\n", customer.CustName, customer.CustDes)
			continue
		}

		totalItems += len(accountReceivables)
		fmt.Printf("\nFound %d account receivable item(s) for customer: %s (%s)\n\n", len(accountReceivables), customer.CustName, customer.CustDes)
		fmt.Println(strings.Repeat("=", 82))

		for i, item := range accountReceivables {
			fmt.Printf("\nAccount Receivable Item #%d:\n", i+1)
			itemJSON, err := json.MarshalIndent(item, "  ", "  ")
			if err != nil {
				fmt.Printf("  Error formatting account receivable item: %v\n", err)
				continue
			}
			fmt.Println(string(itemJSON))
		}

		fmt.Println("\n" + strings.Repeat("=", 82))
	}

	if totalItems == 0 {
		fmt.Printf("\nNo account receivables found for email: %s\n", email)
	}
}

func getCustomerFn(cmd *cobra.Command, args []string) {
	email, err := cmd.Flags().GetString("email")
	if err != nil {
		slog.Error("Failed to read email flag", slog.Any("error", err))
		os.Exit(1)
	}
	if email == "" {
		fmt.Println("Email is required (use --email flag)")
		os.Exit(1)
	}

	// Check for required Priority credentials in environment/config
	if common.Config.PriorityBaseURL == "" {
		slog.Error("PRIORITY_BASE_URL environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityUsername == "" {
		slog.Error("PRIORITY_USERNAME environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityPassword == "" {
		slog.Error("PRIORITY_PASSWORD environment variable is required")
		os.Exit(1)
	}

	// Create Priority client
	client := priority.NewClient()

	ctx := context.Background()

	slog.Info("Fetching customers from Priority ERP", slog.String("email", email))

	customers, err := client.GetCustomersByEmail(ctx, email)
	if err != nil {
		slog.Error("Failed to fetch customers", slog.Any("error", err))
		os.Exit(1)
	}

	if len(customers) == 0 {
		fmt.Printf("\nNo customers found for email: %s\n", email)
		return
	}

	fmt.Printf("\nFound %d customer(s) for email: %s\n", len(customers), email)
	fmt.Println(strings.Repeat("=", 82))

	for i, customer := range customers {
		fmt.Printf("\nCustomer #%d (Status: %s):\n", i+1, customer.StatDes)
		customerJSON, err := json.MarshalIndent(customer, "  ", "  ")
		if err != nil {
			fmt.Printf("  Error formatting customer: %v\n", err)
			continue
		}
		fmt.Println(string(customerJSON))
	}

	fmt.Println("\n" + strings.Repeat("=", 82))
}

func getCustomerByIDFn(cmd *cobra.Command, args []string) {
	customerID := args[0]

	if common.Config.PriorityBaseURL == "" {
		slog.Error("PRIORITY_BASE_URL environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityUsername == "" {
		slog.Error("PRIORITY_USERNAME environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityPassword == "" {
		slog.Error("PRIORITY_PASSWORD environment variable is required")
		os.Exit(1)
	}

	client := priority.NewClient()
	ctx := context.Background()

	slog.Info("Fetching customer from Priority ERP", slog.String("customerID", customerID))

	customer, err := client.GetCustomerByID(ctx, customerID)
	if err != nil {
		slog.Error("Failed to fetch customer", slog.Any("error", err))
		os.Exit(1)
	}
	if customer == nil {
		fmt.Printf("\nNo customer found for ID: %s\n", customerID)
		return
	}

	fmt.Printf("\nCustomer %s (Status: %s):\n", customer.CustName, customer.StatDes)
	fmt.Println(strings.Repeat("=", 82))
	customerJSON, err := json.MarshalIndent(customer, "  ", "  ")
	if err != nil {
		slog.Error("Failed to format customer", slog.Any("error", err))
		os.Exit(1)
	}
	fmt.Println(string(customerJSON))
	fmt.Println("\n" + strings.Repeat("=", 82))
}

func accountReceivablesByIDFn(cmd *cobra.Command, args []string) {
	customerID := args[0]

	if common.Config.PriorityBaseURL == "" {
		slog.Error("PRIORITY_BASE_URL environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityUsername == "" {
		slog.Error("PRIORITY_USERNAME environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityPassword == "" {
		slog.Error("PRIORITY_PASSWORD environment variable is required")
		os.Exit(1)
	}

	client := priority.NewClient()
	ctx := context.Background()

	slog.Info("Fetching account receivables from Priority ERP", slog.String("customerID", customerID))

	items, err := client.GetAccountReceivables(ctx, customerID)
	if err != nil {
		slog.Error("Failed to fetch account receivables", slog.Any("error", err))
		os.Exit(1)
	}
	if len(items) == 0 {
		fmt.Printf("\nNo account receivables found for customer ID: %s\n", customerID)
		return
	}

	fmt.Printf("\nFound %d account receivable item(s) for customer ID: %s\n\n", len(items), customerID)
	fmt.Println(strings.Repeat("=", 82))
	for i, item := range items {
		fmt.Printf("\nAccount Receivable Item #%d:\n", i+1)
		itemJSON, err := json.MarshalIndent(item, "  ", "  ")
		if err != nil {
			fmt.Printf("  Error formatting account receivable item: %v\n", err)
			continue
		}
		fmt.Println(string(itemJSON))
	}
	fmt.Println("\n" + strings.Repeat("=", 82))
}

func lastContributionsFn(cmd *cobra.Command, args []string) {
	emails := make([]string, 0, len(args))
	for _, arg := range args {
		for _, e := range strings.Split(arg, ",") {
			if e = strings.TrimSpace(e); e != "" {
				emails = append(emails, e)
			}
		}
	}
	if len(emails) == 0 {
		fmt.Println("No emails provided")
		os.Exit(1)
	}

	// Validate configuration
	if common.Config.PriorityBaseURL == "" {
		slog.Error("PRIORITY_BASE_URL environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityUsername == "" {
		slog.Error("PRIORITY_USERNAME environment variable is required")
		os.Exit(1)
	}
	if common.Config.PriorityPassword == "" {
		slog.Error("PRIORITY_PASSWORD environment variable is required")
		os.Exit(1)
	}

	client := priority.NewClient()
	ctx := context.Background()

	slog.Info("Fetching last contributions (batch) from Priority ERP", slog.Int("emails", len(emails)))

	result, err := client.GetLastContributionsBatch(ctx, emails)
	if err != nil {
		slog.Error("Failed to fetch last contributions batch", slog.Any("error", err))
		os.Exit(1)
	}

	// This CLI treats every given email as one group (i.e. aliases of the same person),
	// so a single SumGroup call over the whole list gives the total without double-counting
	// a customer matched by more than one of the given emails. A real batch job with many
	// people would instead call SumGroup once per person's group of emails, against this
	// same fetched result -- no extra Priority calls needed either way.
	sums := result.SumGroup(emails)

	fmt.Printf("\nLast 12 months contributions for %d email(s)\n", len(emails))
	fmt.Println(strings.Repeat("=", 82))

	if len(sums) == 0 {
		fmt.Println("\n(no contributions found)")
	}
	for currency, amount := range sums {
		fmt.Printf("\nCurrency: %s\n", currency)
		fmt.Printf("  Total Amount: %.2f\n", amount)
	}

	fmt.Println("\n" + strings.Repeat("=", 82))
	fmt.Printf("\nPriority requests: %d | bytes received: %d | duration: %s\n",
		result.Stats.Requests, result.Stats.Bytes, result.Stats.Duration)
}
