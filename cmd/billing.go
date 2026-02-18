package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/billing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
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

func init() {
	rootCmd.AddCommand(billingCmd)
	billingCmd.AddCommand(billingStartCmd)

	// Flags for billing start
	billingStartCmd.Flags().Int("month", 0, "Month (1-12), defaults to current month")
	billingStartCmd.Flags().Int("year", 0, "Year, defaults to current year")
	billingStartCmd.Flags().Bool("flags", true, "Clear all flags before processing")
	billingStartCmd.Flags().Bool("muhlafim", true, "Process muhlafim")
	billingStartCmd.Flags().Bool("charge", true, "Charge orders")
	billingStartCmd.Flags().Bool("dry-run", false, "Run full workflow and all DB operations; only the payment terminal call is simulated (15%% total fail, 30%% fail first terminal, 50%% fail second). Use with a local/production-copy DB.")
	billingStartCmd.Flags().Bool("use-concurrent", true, "Use concurrent charging, defaults to true")
	billingStartCmd.Flags().Int("max-workers", 5, "Maximum number of workers to use for concurrent charging, defaults to 5")
}

func runBillingStart(cmd *cobra.Command, args []string) {
	slog.Info("Running complete monthly billing workflow", slog.String("command", "billing start"))

	// Setup sentry
	sentryTransport := sentry.NewHTTPSyncTransport()
	sentryTransport.Timeout = 3 * time.Second
	err := sentry.Init(sentry.ClientOptions{
		Release:     common.GitSHA,
		Environment: common.Config.Env,
		Transport:   sentryTransport,
		Tags: map[string]string{
			"command": "billing start",
		},
	})
	if err != nil {
		utils.LogFatal("sentry.Init", slog.Any("err", err))
	}
	defer sentry.Flush(2 * time.Second)

	// Parse flags
	month, year, opts, err := parseFlagsBillingStart(cmd)
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to parse flags", slog.Any("error", err))
	}

	// Initialize event emitter
	eventEmitter, err := events.CreateEmitter()
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to create event emitter", slog.Any("error", err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eventEmitter.Close(ctx)
	}()

	// Initialize database
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ordersDB, err := repo.NewOrdersDB(ctx, eventEmitter)
	if err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Failed to initialize database", slog.Any("error", err))
	}
	defer ordersDB.Close()

	if opts.DryRun {
		ordersDB.SetDryRunChargeExecutor(repo.NewDryRunChargeExecutor())
	}

	// Run workflow
	ctx = context.WithValue(context.Background(), common.CtxEventBuilder, new(BillingWorkflowEventBuilder))
	billingService := billing.NewBillingService(ordersDB, pelecard.NewClient())
	if err := billingService.RunBillingWorkflow(ctx, month, year, opts); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("Billing workflow failed", slog.Any("error", err))
	}

	slog.Info("Billing workflow completed successfully")
}

type BillingWorkflowEventBuilder struct{}

func (b *BillingWorkflowEventBuilder) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentBillingWorkflow
	event.Actor = events.ActorSystem
	return event
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
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return 0, 0, nil, fmt.Errorf("malformed dry-run flag: %w", err)
	}
	useConcurrent, err := cmd.Flags().GetBool("use-concurrent")
	if err != nil {
		return 0, 0, nil, fmt.Errorf("malformed use-concurrent flag: %w", err)
	}
	maxWorkers, err := cmd.Flags().GetInt("max-workers")
	if err != nil {
		return 0, 0, nil, fmt.Errorf("malformed max-workers flag: %w", err)
	}
	if maxWorkers <= 0 {
		return 0, 0, nil, fmt.Errorf("max-workers must be greater than 0, got: %d", maxWorkers)
	}

	return month, year, &billing.WorkflowOptions{
		Flags:         flags,
		Muhlafim:      muhlafim,
		Charge:        charge,
		DryRun:        dryRun,
		UseConcurrent: useConcurrent,
		MaxWorkers:    maxWorkers,
	}, nil
}
