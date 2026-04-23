package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// BillingService orchestrates the billing workflow
type BillingService struct {
	repo           repo.OrdersRepository
	pelecardClient pelecard.PelecardAPI
	eventEmitter   events.EventEmitter
	resolver       *pricing.PriceResolver
	chargeExecutor pelecard.ChargeExecutor
}

// NewBillingService creates a new billing service
func NewBillingService(
	repo repo.OrdersRepository,
	pelecardClient pelecard.PelecardAPI,
	eventEmitter events.EventEmitter,
	resolver *pricing.PriceResolver,
	chargeExecutor pelecard.ChargeExecutor,
) *BillingService {
	return &BillingService{
		repo:           repo,
		pelecardClient: pelecardClient,
		eventEmitter:   eventEmitter,
		resolver:       resolver,
		chargeExecutor: chargeExecutor,
	}
}

// WorkflowOptions contains optional parameters for the billing workflow
type WorkflowOptions struct {
	Flags    bool
	Muhlafim bool
	Charge   bool
	// DryRun when true means the payment terminal call is simulated (no live gateway); all DB operations are real.
	DryRun bool
	// MaxWorkers sets the number of concurrent workers for the charge phase.
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

// RetryPricingErrors retries pricing resolution and charging for orders previously flagged as pricing_error.
// It is designed to be called as a standalone operation — separate from the main billing workflow.
func (s *BillingService) RetryPricingErrors(ctx context.Context, maxWorkers int) (int, error) {
	log := utils.LogFor(ctx)
	log.Info("=== Retrying Pricing Errors ===")

	orderIDs, err := s.repo.GetOrderIDsWithPricingError(ctx)
	if err != nil {
		return 0, fmt.Errorf("GetOrderIDsWithPricingError: %w", err)
	}
	if len(orderIDs) == 0 {
		log.Info("No orders with pricing errors to retry")
		return 0, nil
	}
	log.Info("Found orders with pricing errors", slog.Int("count", len(orderIDs)))

	resolved, pricingErrors := s.preResolve(ctx, orderIDs, maxWorkers)
	log.Info("Retry pre-resolution complete",
		slog.Int("resolved", len(resolved)),
		slog.Int("still_failing", pricingErrors))

	if len(resolved) == 0 {
		log.Warn("All orders still fail pricing resolution")
		return 0, nil
	}

	// Transition pricing_error → torenew before charge so a subsequent card decline
	// doesn't leave orders looking like they still have a pricing error. Fatal on
	// failure: this is the retry's semantic contract, not housekeeping.
	resolvedIDs := make([]uint, len(resolved))
	for i, ro := range resolved {
		resolvedIDs[i] = ro.OrderID
	}
	if err := s.repo.MarkResolvedForRenew(ctx, resolvedIDs); err != nil {
		return 0, fmt.Errorf("MarkResolvedForRenew: %w", err)
	}

	count, err := s.chargeResolved(ctx, resolved, maxWorkers)
	if err != nil {
		return 0, fmt.Errorf("chargeResolved: %w", err)
	}

	log.Info("Retry pricing errors completed", slog.Int("orders_charged", count))
	return count, nil
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

// chargeOperations handles charging orders using pricing-aware pre-resolution + charge.
func (s *BillingService) chargeOperations(ctx context.Context, opts *WorkflowOptions) error {
	if opts != nil && !opts.Charge {
		utils.LogFor(ctx).Info("Skipping charging operations")
		return nil
	}
	utils.LogFor(ctx).Info("Step 3: Charging flagged orders", slog.Bool("dry-run", opts != nil && opts.DryRun))

	maxWorkers := getDefaultMaxWorkers()
	if opts != nil && opts.MaxWorkers > 0 {
		maxWorkers = opts.MaxWorkers
	}

	count, err := s.chargeWithPricing(ctx, maxWorkers)
	if err != nil {
		return fmt.Errorf("charge with pricing: %w", err)
	}
	utils.LogFor(ctx).Info("Completed charging orders", slog.Int("orders_charged", count))
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
