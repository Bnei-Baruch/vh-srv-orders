package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// BillingService orchestrates the billing workflow
type BillingService struct {
	repo           repo.OrdersRepository
	pelecardClient pelecard.PelecardAPI
}

// NewBillingService creates a new billing service
func NewBillingService(repo repo.OrdersRepository, pelecardClient pelecard.PelecardAPI) *BillingService {
	return &BillingService{
		repo:           repo,
		pelecardClient: pelecardClient,
	}
}

// WorkflowOptions contains optional parameters for the billing workflow
type WorkflowOptions struct {
	Flags    bool
	Muhlafim bool
	Charge   bool
	// DryRun when true means the payment terminal call is simulated (no live gateway); all DB operations are real.
	DryRun bool
	// UseConcurrent enables concurrent order processing with goroutines.
	// When enabled, ChargeOrdersConcurrent is used instead of ChargeOrdersToRenew.
	UseConcurrent bool
	// MaxWorkers sets the number of concurrent workers when UseConcurrent or UseFullCycle is true.
	// If <= 0, defaults to 5 (or RENEWAL_MAX_WORKERS env var).
	MaxWorkers int
}

// RunBillingWorkflow runs the complete billing workflow for the specified month/year
func (s *BillingService) RunBillingWorkflow(ctx context.Context, month, year int, opts *WorkflowOptions) error {
	utils.LogFor(ctx).Info("=== Starting Billing Workflow ===",
		slog.Int("month", month),
		slog.Int("year", year),
		slog.Any("opts", opts))

	period := NewBillingPeriodWithDate(month, year)

	// Step 1: Flag & Skip Operations
	if err := s.flagAndSkipOperations(ctx, period, opts); err != nil {
		return fmt.Errorf("Step 1: flag and skip operations: %w", err)
	}
	if err := s.logOrdersCountByStatus(ctx); err != nil {
		return fmt.Errorf("log orders count by status: %w", err)
	}

	// Step 2: Muhlafim Processing - Fetch card updates from Pelecard
	if err := s.processMuhlafim(ctx, period, opts); err != nil {
		return fmt.Errorf("Step 2: process muhlafim: %w", err)
	}
	if err := s.logOrdersCountByStatus(ctx); err != nil {
		return fmt.Errorf("log orders count by status: %w", err)
	}

	// Step 3: Charge Operations
	if err := s.chargeOperations(ctx, opts); err != nil {
		return fmt.Errorf("Step 3: charge operations: %w", err)
	}

	utils.LogFor(ctx).Info("=== Billing Workflow Completed Successfully ===")
	return nil
}

// flagAndSkipOperations handles flagging orders and skipping operations
func (s *BillingService) flagAndSkipOperations(ctx context.Context, period *BillingPeriod, opts *WorkflowOptions) error {
	// Clear flags if requested
	if opts != nil && !opts.Flags {
		utils.LogFor(ctx).Info("Skipping flagging and skipping operations")
		return nil
	}
	utils.LogFor(ctx).Info("Step 1: Flagging orders for renewal and applying skip rules")

	utils.LogFor(ctx).Info("Clearing all existing order flags")
	if err := s.repo.ClearAllFlags(ctx); err != nil {
		return fmt.Errorf("clear flags: %w", err)
	}

	// Update user keys
	utils.LogFor(ctx).Info("Synchronizing user keys from accounts table to orders")
	if err := s.repo.UpdateOrdersUserKeyFromAccounts(ctx); err != nil {
		return fmt.Errorf("update user keys: %w", err)
	}

	// Flag orders for renewal
	utils.LogFor(ctx).Info("Identifying and flagging orders eligible for renewal")
	count, err := s.repo.FlagOrdersToRenew(ctx, int64(period.Month), int64(period.Year))
	if err != nil {
		return fmt.Errorf("flag orders for renewal: %w", err)
	}
	utils.LogFor(ctx).Info("Orders flagged for renewal", slog.Int64("count", count))

	// Skip double orders
	lastDayLastMonth := period.GetLastDayOfLastMonth()
	lastMonth := period.Month - 1
	lastMonthYear := period.Year
	if lastMonth == 0 {
		lastMonth = 12
		lastMonthYear--
	}

	utils.LogFor(ctx).Info("Applying skip rule: excluding users with multiple orders from last month",
		slog.Int("last_month_year", lastMonthYear),
		slog.Int("last_month", lastMonth),
		slog.String("last_day_last_month", lastDayLastMonth.Format(time.DateTime)))
	skipped, err := SkipDoubleOrders(ctx, s.repo, lastMonthYear, lastMonth, lastDayLastMonth)
	if err != nil {
		return fmt.Errorf("skip double orders: %w", err)
	}
	utils.LogFor(ctx).Info("Skipped orders for users with double payments", slog.Int("orders_skipped", skipped))

	// Skip fresh orders
	lastDayThisMonth := period.GetEndOfMonth()
	utils.LogFor(ctx).Info("Applying skip rule: excluding users who already paid this month")
	skipped, err = SkipFreshOrders(ctx, s.repo, period.Year, period.Month, lastDayThisMonth)
	if err != nil {
		return fmt.Errorf("skip fresh orders: %w", err)
	}
	utils.LogFor(ctx).Info("Skipped orders for users who already paid", slog.Int("orders_skipped", skipped))

	return nil
}

// processMuhlafim fetches and processes muhlafim data from Pelecard
func (s *BillingService) processMuhlafim(ctx context.Context, period *BillingPeriod, opts *WorkflowOptions) error {
	if opts != nil && !opts.Muhlafim {
		utils.LogFor(ctx).Info("Skipping muhlafim processing")
		return nil
	}
	utils.LogFor(ctx).Info("Step 2: Processing muhlafim - fetching card status updates from Pelecard",
		slog.Bool("dry-run", opts != nil && opts.DryRun))

	muhlafimStartDate := period.GetMuhlafimStartDate()
	muhlafimEndDate := period.GetMuhlafimEndDate()

	utils.LogFor(ctx).Info("Fetching muhlafim data from Pelecard",
		slog.String("start_date", muhlafimStartDate.Format(time.DateTime)),
		slog.String("end_date", muhlafimEndDate.Format(time.DateTime)))

	dryRun := opts != nil && opts.DryRun
	result, err := ProcessMuhlafim(ctx, s.repo, s.pelecardClient, muhlafimStartDate, muhlafimEndDate, false, dryRun)
	if err != nil {
		return fmt.Errorf("process muhlafim: %w", err)
	}

	utils.LogFor(ctx).Info("Muhlafim processing completed",
		slog.Int("orders_processed", result.Processed),
		slog.Int("orders_updated", result.Updated),
		slog.Int("new_cards_detected", result.NewCards),
		slog.Any("flags", result.Flags))

	return nil
}

// chargeOperations handles charging orders
func (s *BillingService) chargeOperations(ctx context.Context, opts *WorkflowOptions) error {
	if opts != nil && !opts.Charge {
		utils.LogFor(ctx).Info("Skipping charging operations")
		return nil
	}
	utils.LogFor(ctx).Info("Step 3: Charging flagged orders", slog.Bool("dry-run", opts != nil && opts.DryRun))

	if opts != nil && opts.UseConcurrent {
		// Charge orders concurrently, full terminal fallback (token -> emv)
		count, err := chargeOrdersConcurrent(ctx, s.repo, opts.MaxWorkers)
		if err != nil {
			return fmt.Errorf("charge orders concurrently: %w", err)
		}
		utils.LogFor(ctx).Info("Completed charging orders concurrently", slog.Int("orders_charged", count))
	} else {
		// Charge "masof oorat keva" (key="t")
		utils.LogFor(ctx).Info("Charging orders: masof oorat keva (token)")
		count, err := s.repo.ChargeOrdersToRenew(ctx, pelecard.TokenTerminal.PMX)
		if err != nil {
			return fmt.Errorf("charge masof oorat keva: %w", err)
		}
		utils.LogFor(ctx).Info("Completed charging masof oorat keva", slog.Int("orders_charged", count))

		// Charge "masof ragil" (key="e")
		utils.LogFor(ctx).Info("Charging orders: masof ragil (emv)")
		count, err = s.repo.ChargeOrdersToRenew(ctx, pelecard.EMVTerminal.PMX)
		if err != nil {
			return fmt.Errorf("charge masof ragil: %w", err)
		}
		utils.LogFor(ctx).Info("Completed charging masof ragil", slog.Int("orders_charged", count))
	}
	return nil
}

// logOrdersCountByStatus logs the count of flagged orders by status
func (s *BillingService) logOrdersCountByStatus(ctx context.Context) error {
	flaggedOrders, err := s.repo.GetFlaggedOrders(ctx)
	if err != nil {
		return fmt.Errorf("s.repo.GetFlaggedOrders: %w", err)
	}

	byStatus := make(map[string]int)
	for _, order := range flaggedOrders {
		byStatus[order.Status.String]++
	}

	utils.LogFor(ctx).Info("Flagged orders count by status",
		slog.Int("count", len(flaggedOrders)),
		slog.Any("by_status", byStatus))

	return nil
}
