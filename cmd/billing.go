package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/billing"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

var billingCmd = &cobra.Command{
	Use:   "billing",
	Short: "Recurring billing operations",
	Long:  "Commands for managing recurring billing operations",
}

var billingStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Run complete billing workflow",
	Long:  "Runs the complete billing workflow: flag orders, process muhlafim, and charge payments",
	Run:   runBillingStart,
}

var billingRetryPricingErrorsCmd = &cobra.Command{
	Use:   "retry-pricing-errors",
	Short: "Retry orders that failed pricing resolution",
	Long:  "Retries pricing resolution and charging for orders previously flagged as pricing_error. Does not re-run flag/muhlafim steps.",
	Run:   runBillingRetryPricingErrors,
}

var billingReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Reconcile orders charged but not finalized in DB",
	Long: `Reads a log file for CHARGE_SUCCESS_DB_FAIL markers and retries FinalizeRenewal for each.
Use after a billing run that had post-payment DB failures.

Examples:
  orders billing reconcile --log-file /var/log/billing.log
  grep CHARGE_SUCCESS_DB_FAIL /var/log/billing.log | orders billing reconcile --log-file -`,
	Run: runBillingReconcile,
}

func init() {
	rootCmd.AddCommand(billingCmd)
	billingCmd.AddCommand(billingStartCmd)
	billingCmd.AddCommand(billingRetryPricingErrorsCmd)
	billingCmd.AddCommand(billingReconcileCmd)

	// Shared flags inherited by all billing subcommands
	billingCmd.PersistentFlags().Bool("dry-run", false, "Simulate payment gateway calls (no live charges); all DB operations are real")
	billingCmd.PersistentFlags().Int("max-workers", 5, "Maximum number of concurrent charge workers")

	// reconcile-specific flags
	billingReconcileCmd.Flags().String("log-file", "", "Path to log file with CHARGE_SUCCESS_DB_FAIL markers, or - for stdin")
	_ = billingReconcileCmd.MarkFlagRequired("log-file")

	// start-specific flags
	billingStartCmd.Flags().Int("month", 0, "Month (1-12), defaults to current month")
	billingStartCmd.Flags().Int("year", 0, "Year, defaults to current year")
	billingStartCmd.Flags().Bool("flags", true, "Clear all flags before processing")
	billingStartCmd.Flags().Bool("muhlafim", true, "Process muhlafim")
	billingStartCmd.Flags().Bool("charge", true, "Charge orders")
}

func runBillingStart(cmd *cobra.Command, args []string) {
	slog.Info("Running complete monthly billing workflow", slog.String("command", "billing start"))
	initBillingSentry("billing start")
	defer sentry.Flush(2 * time.Second)

	month, year, opts, err := parseFlagsBillingStart(cmd)
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to parse flags", slog.Any("error", err))
	}

	eventEmitter, ordersDB, cleanup, err := initBillingInfra()
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to initialize billing infrastructure", slog.Any("error", err))
	}
	defer cleanup()

	billingService := buildChargeableBillingService(ordersDB, eventEmitter, opts.DryRun)
	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, new(BillingWorkflowEventBuilder))
	if err := billingService.RunBillingWorkflow(ctx, month, year, opts); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Billing workflow failed", slog.Any("error", err))
	}

	slog.Info("Billing workflow completed successfully")
}

func runBillingRetryPricingErrors(cmd *cobra.Command, args []string) {
	slog.Info("Retrying orders with pricing errors", slog.String("command", "billing retry-pricing-errors"))
	initBillingSentry("billing retry-pricing-errors")
	defer sentry.Flush(2 * time.Second)

	dryRun, maxWorkers, err := parseSharedBillingFlags(cmd)
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to parse flags", slog.Any("error", err))
	}

	eventEmitter, ordersDB, cleanup, err := initBillingInfra()
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to initialize billing infrastructure", slog.Any("error", err))
	}
	defer cleanup()

	billingService := buildChargeableBillingService(ordersDB, eventEmitter, dryRun)
	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, new(BillingWorkflowEventBuilder))
	count, err := billingService.RetryPricingErrors(ctx, maxWorkers)
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Retry pricing errors failed", slog.Any("error", err))
	}

	slog.Info("Retry pricing errors completed successfully", slog.Int("orders_charged", count))
}

func runBillingReconcile(cmd *cobra.Command, args []string) {
	slog.Info("Running billing reconciliation", slog.String("command", "billing reconcile"))
	initBillingSentry("billing reconcile")
	defer sentry.Flush(2 * time.Second)

	logFile, err := cmd.Flags().GetString("log-file")
	if err != nil {
		utils.LogFatal("malformed log-file flag", slog.Any("err", err))
	}

	var reader io.Reader
	if logFile == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(logFile)
		if err != nil {
			utils.LogFatal("Failed to open log file", slog.String("path", logFile), slog.Any("err", err))
		}
		defer f.Close()
		reader = f
	}

	entries, err := billing.ParseReconcileEntries(reader)
	if err != nil {
		utils.LogFatal("Failed to parse log entries", slog.Any("err", err))
	}
	if len(entries) == 0 {
		slog.Info("No CHARGE_SUCCESS_DB_FAIL entries found in log")
		return
	}
	slog.Info("Parsed reconcile entries from log", slog.Int("count", len(entries)))

	eventEmitter, ordersDB, cleanup, err := initBillingInfra()
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to initialize billing infrastructure", slog.Any("error", err))
	}
	defer cleanup()

	billingService := billing.NewBillingService(ordersDB, nil, eventEmitter, nil, nil)
	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, new(BillingWorkflowEventBuilder))
	result := billingService.Reconcile(ctx, entries)

	slog.Info("Reconciliation completed",
		slog.Int("total", result.Total),
		slog.Int("reconciled", result.Reconciled),
		slog.Int("already_reconciled", result.AlreadyReconciled),
		slog.Int("failed", result.Failed))

	if result.Failed > 0 {
		sentry.CaptureMessage(fmt.Sprintf("billing reconcile: %d/%d entries failed", result.Failed, result.Total))
	}
}

// BillingWorkflowEventBuilder builds events tagged with the billing workflow component and system actor.
type BillingWorkflowEventBuilder struct{}

func (b *BillingWorkflowEventBuilder) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentBillingWorkflow
	event.Actor = events.ActorSystem
	return event
}

// initBillingSentry initializes Sentry with a synchronous transport tagged for the given command.
// Caller should defer sentry.Flush(2 * time.Second).
func initBillingSentry(commandName string) {
	sentryTransport := sentry.NewHTTPSyncTransport()
	sentryTransport.Timeout = 3 * time.Second
	if err := sentry.Init(sentry.ClientOptions{
		Release:     common.GitSHA,
		Environment: common.Config.Env,
		Transport:   sentryTransport,
		Tags:        map[string]string{"command": commandName},
	}); err != nil {
		utils.LogFatal("sentry.Init", slog.Any("err", err))
	}
}

// initBillingInfra creates the event emitter and database connection pool.
// The returned cleanup func closes both in the correct order; callers should defer it.
func initBillingInfra() (events.EventEmitter, *repo.OrdersDB, func(), error) {
	eventEmitter, err := events.CreateEmitter()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("events.CreateEmitter: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ordersDB, err := repo.NewOrdersDB(ctx, eventEmitter)
	if err != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		eventEmitter.Close(shutdownCtx)
		return nil, nil, nil, fmt.Errorf("repo.NewOrdersDB: %w", err)
	}

	cleanup := func() {
		ordersDB.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eventEmitter.Close(ctx)
	}
	return eventEmitter, ordersDB, cleanup, nil
}

// buildChargeableBillingService wires a BillingService with charge executor and pricing resolver.
// Used by commands that perform charging (start, retry-pricing-errors).
func buildChargeableBillingService(ordersDB *repo.OrdersDB, eventEmitter events.EventEmitter, dryRun bool) *billing.BillingService {
	if err := pricing.ValidateConfig(); err != nil {
		utils.LogFatal("pricing.ValidateConfig", slog.Any("err", err))
	}

	pelecardClient := pelecard.NewClient()
	var chargeExecutor pelecard.ChargeExecutor
	if dryRun {
		chargeExecutor = pelecard.NewDryRunChargeExecutor()
	} else {
		chargeExecutor = pelecardClient
	}
	profileService := profiles.NewProfileServiceAPI(keycloak.NewClient())
	priorityClient := priority.NewClient()
	priorityClient.SetCacheEnabled(true)
	accountingClient := accounting.NewAccountingServiceAPI(keycloak.NewClient())
	accountingClient.SetCacheEnabled(true)
	resolver := pricing.NewPriceResolver(profileService, priorityClient, accountingClient, common.Config.QuickbooksCompanyID)
	return billing.NewBillingService(ordersDB, pelecardClient, eventEmitter, resolver, chargeExecutor)
}

// parseSharedBillingFlags reads the dry-run and max-workers flags common to all charge commands.
func parseSharedBillingFlags(cmd *cobra.Command) (dryRun bool, maxWorkers int, err error) {
	dryRun, err = cmd.Flags().GetBool("dry-run")
	if err != nil {
		return false, 0, fmt.Errorf("malformed dry-run flag: %w", err)
	}
	maxWorkers, err = cmd.Flags().GetInt("max-workers")
	if err != nil {
		return false, 0, fmt.Errorf("malformed max-workers flag: %w", err)
	}
	if maxWorkers <= 0 {
		return false, 0, fmt.Errorf("max-workers must be greater than 0, got: %d", maxWorkers)
	}
	return dryRun, maxWorkers, nil
}

func parseFlagsBillingStart(cmd *cobra.Command) (int, int, *billing.WorkflowOptions, error) {
	now := time.Now()
	month := int(now.Month())
	year := now.Year()

	if cmd.Flags().Changed("month") {
		m, err := cmd.Flags().GetInt("month")
		if err != nil {
			return 0, 0, nil, fmt.Errorf("malformed month flag: %w", err)
		}
		if m >= 1 && m <= 12 {
			month = m
		} else {
			return 0, 0, nil, fmt.Errorf("month must be between 1 and 12, got: %d", m)
		}
	}

	if cmd.Flags().Changed("year") {
		y, err := cmd.Flags().GetInt("year")
		if err != nil {
			return 0, 0, nil, fmt.Errorf("malformed year flag: %w", err)
		}
		if y < 2026 || y > year {
			return 0, 0, nil, fmt.Errorf("year must be between 2026 and %d", year)
		}
		year = y
	}

	flags, err := cmd.Flags().GetBool("flags")
	if err != nil {
		return 0, 0, nil, fmt.Errorf("malformed flags flag: %w", err)
	}
	muhlafim, err := cmd.Flags().GetBool("muhlafim")
	if err != nil {
		return 0, 0, nil, fmt.Errorf("malformed muhlafim flag: %w", err)
	}
	charge, err := cmd.Flags().GetBool("charge")
	if err != nil {
		return 0, 0, nil, fmt.Errorf("malformed charge flag: %w", err)
	}

	dryRun, maxWorkers, err := parseSharedBillingFlags(cmd)
	if err != nil {
		return 0, 0, nil, err
	}

	return month, year, &billing.WorkflowOptions{
		Flags:      flags,
		Muhlafim:   muhlafim,
		Charge:     charge,
		DryRun:     dryRun,
		MaxWorkers: maxWorkers,
	}, nil
}
