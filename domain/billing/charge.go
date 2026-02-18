package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/getsentry/sentry-go"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// chargeStats tracks renewal statistics (access must be thread-safe)
type chargeStats struct {
	totalOrders  int64
	successCount *utils.CounterMap[int64]
	successSum   *utils.CounterMap[float64]
	failedCount  *utils.CounterMap[int64]   // orders declined on all terminals
	failedSum    *utils.CounterMap[float64] // declined order amounts by currency
	errorCount   *utils.CounterMap[int64]   // pre_payment, post_payment, gateway, panic
}

// chargeOrdersConcurrent processes orders concurrently with terminal fallback (token -> emv).
// It creates a worker pool that processes orders concurrently with panic recovery per worker.
// Context cancellation is checked before starting each new order.
//
// This is a best-effort operation: individual order failures are logged and reported to Sentry
// but do not abort the batch. The caller should inspect the returned count to determine
// how many orders were successfully renewed out of the total.
//
// Returns: (successful renewal count, error)
func chargeOrdersConcurrent(ctx context.Context, ordersRepo repo.OrdersRepository, maxWorkers int) (int, error) {
	if maxWorkers <= 0 {
		maxWorkers = getDefaultMaxWorkers()
	}

	utils.LogFor(ctx).Info("Starting concurrent order renewal", slog.Int("max_workers", maxWorkers))

	orderIDs, err := ordersRepo.GetOrderIDsToRenew(ctx)
	if err != nil {
		return 0, fmt.Errorf("get order ids to renew: %w", err)
	}

	stats := chargeStats{
		totalOrders:  int64(len(orderIDs)),
		successCount: utils.NewCounterMap[int64](),
		successSum:   utils.NewCounterMap[float64](),
		failedCount:  utils.NewCounterMap[int64](),
		failedSum:    utils.NewCounterMap[float64](),
		errorCount:   utils.NewCounterMap[int64](),
	}
	if stats.totalOrders == 0 {
		utils.LogFor(ctx).Info("No orders to renew")
		return 0, nil
	}
	utils.LogFor(ctx).Info("Orders collected for renewal", slog.Int64("total_orders", stats.totalOrders))

	orderChan := make(chan uint, maxWorkers)
	var wg sync.WaitGroup

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for orderID := range orderChan {
				select {
				case <-ctx.Done():
					utils.LogFor(ctx).Info("Context cancelled, worker exiting",
						slog.Int("worker_id", workerID),
						slog.Uint64("order_id", uint64(orderID)))
					return
				default:
					processOrderWithRecovery(ctx, ordersRepo, workerID, orderID, &stats)
				}
			}
		}(i)
	}

dispatchLoop:
	for _, orderID := range orderIDs {
		select {
		case <-ctx.Done():
			utils.LogFor(ctx).Info("Context cancelled, stopping order dispatch")
			break dispatchLoop
		case orderChan <- orderID:
		}
	}

	close(orderChan)
	wg.Wait()

	successCount := stats.successCount.Get(pelecard.TokenTerminal.Name) + stats.successCount.Get(pelecard.EMVTerminal.Name)

	if successCount == 0 && stats.totalOrders > 0 {
		utils.LogFor(ctx).Warn("All order renewals failed", slog.Int64("total_orders", stats.totalOrders))
	}

	utils.LogFor(ctx).Info("Renewal completed",
		slog.Int64("total_orders", stats.totalOrders),
		slog.Int64("successful_renewals", successCount),
		slog.Int64("via_token", stats.successCount.Get(pelecard.TokenTerminal.Name)),
		slog.Int64("via_emv", stats.successCount.Get(pelecard.EMVTerminal.Name)),
		slog.Float64("success_nis", stats.successSum.Get(common.CurrencyNIS)),
		slog.Float64("success_usd", stats.successSum.Get(common.CurrencyUSD)),
		slog.Float64("success_eur", stats.successSum.Get(common.CurrencyEUR)),
		slog.Int64("declined_orders", stats.failedCount.Get("total")),
		slog.Float64("failed_nis", stats.failedSum.Get(common.CurrencyNIS)),
		slog.Float64("failed_usd", stats.failedSum.Get(common.CurrencyUSD)),
		slog.Float64("failed_eur", stats.failedSum.Get(common.CurrencyEUR)),
		slog.Int64("pre_payment_errors", stats.errorCount.Get("pre_payment")),
		slog.Int64("post_payment_errors", stats.errorCount.Get("post_payment")),
		slog.Int64("gateway_errors", stats.errorCount.Get("gateway")),
		slog.Int64("panics", stats.errorCount.Get("panic")))

	return int(successCount), nil
}

// processOrderWithRecovery processes a single order trying token terminal first, then EMV as fallback.
// It uses a non-cancellable context for payment operations to prevent payment state corruption.
func processOrderWithRecovery(ctx context.Context, ordersRepo repo.OrdersRepository, workerID int, orderID uint, stats *chargeStats) {
	ctx = context.WithValue(ctx, common.CtxLogger, utils.LogFor(ctx).With(
		slog.Int("worker_id", workerID),
		slog.Uint64("order_id", uint64(orderID))))

	hub := utils.SentryFor(ctx).Clone()
	hub.Scope().SetExtra("worker_id", workerID)
	hub.Scope().SetExtra("order_id", orderID)
	ctx = sentry.SetHubOnContext(ctx, hub)

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			utils.LogFor(ctx).Error("Panic in worker goroutine",
				slog.Any("panic", r),
				slog.String("stack", stack))
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetExtra("stack", stack)
				hub.CaptureException(fmt.Errorf("panic in order renewal: %v", r))
			})
			stats.errorCount.Inc("panic", 1)
		}
	}()

	paymentCtx := context.WithoutCancel(ctx)

	// === Token Terminal ===
	tokenPayment, tokenErr := ordersRepo.TryRenewalWithTerminal(paymentCtx, orderID, pelecard.TokenTerminal)
	if tokenErr == nil {
		if tokenPayment != nil && tokenPayment.Success.String == "1" {
			stats.successCount.Inc(pelecard.TokenTerminal.Name, 1)
			stats.successSum.Inc(tokenPayment.Currency.String, tokenPayment.Amount.Float64)
			utils.LogFor(ctx).Info("Order renewed successfully", slog.String("terminal", pelecard.TokenTerminal.Name))
			return
		}
		// Token declined — fall through to EMV
	} else if handleNonRetryableError(ctx, hub, stats, pelecard.TokenTerminal.Name, tokenPayment, tokenErr) {
		return
	} else {
		utils.LogFor(ctx).Error("Token terminal gateway error, falling back to EMV", slog.Any("err", tokenErr))
		captureError(hub, pelecard.TokenTerminal.Name, "gateway", tokenErr)
	}

	// === EMV Terminal (fallback) ===
	emvPayment, emvErr := ordersRepo.TryRenewalWithTerminal(paymentCtx, orderID, pelecard.EMVTerminal)
	if emvErr == nil {
		if emvPayment != nil && emvPayment.Success.String == "1" {
			stats.successCount.Inc(pelecard.EMVTerminal.Name, 1)
			stats.successSum.Inc(emvPayment.Currency.String, emvPayment.Amount.Float64)
			utils.LogFor(ctx).Info("Order renewed successfully", slog.String("terminal", pelecard.EMVTerminal.Name))
			return
		}
		recordDeclinedOrder(ctx, stats, emvPayment)
		return
	}

	if handleNonRetryableError(ctx, hub, stats, pelecard.EMVTerminal.Name, emvPayment, emvErr) {
		return
	}

	// Gateway error
	utils.LogFor(ctx).Error("EMV terminal gateway error, all terminals exhausted", slog.Any("err", emvErr))
	captureError(hub, pelecard.EMVTerminal.Name, "gateway", emvErr)
	stats.errorCount.Inc("gateway", 1)
	// Record the failed amount from token decline if token declined (rather than errored)
	if tokenErr == nil && tokenPayment != nil {
		stats.failedSum.Inc(tokenPayment.Currency.String, tokenPayment.Amount.Float64)
	}
}

// handleNonRetryableError handles pre-payment and post-payment errors.
// Returns true if the error was non-retryable (caller should stop), false for gateway errors.
func handleNonRetryableError(ctx context.Context, hub *sentry.Hub, stats *chargeStats, terminal string, payment *repo.Payment, err error) bool {
	if errors.Is(err, common.ErrPrePayment) {
		utils.LogFor(ctx).Error("Pre-payment error - cannot attempt payment",
			slog.String("terminal", terminal),
			slog.Any("err", err))
		captureError(hub, terminal, "pre_payment", err)
		stats.errorCount.Inc("pre_payment", 1)
		return true
	}

	if errors.Is(err, common.ErrPostPayment) {
		recordPostPaymentError(ctx, hub, stats, terminal, payment, err)
		return true
	}

	return false
}

func captureError(hub *sentry.Hub, terminal, errorType string, err error) {
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetExtra("terminal", terminal)
		scope.SetExtra("error_type", errorType)
		hub.CaptureException(err)
	})
}

// recordPostPaymentError handles post-payment errors nil-safely.
// Only records the amount as financially failed when the payment did NOT succeed (Success != "1").
// When Success == "1", the customer was charged but the DB update failed — needs manual reconciliation.
func recordPostPaymentError(ctx context.Context, hub *sentry.Hub, stats *chargeStats, terminal string, payment *repo.Payment, err error) {
	if payment != nil {
		utils.LogFor(ctx).Error("Post-payment error - payment may have succeeded but DB update failed",
			slog.String("terminal", terminal),
			slog.String("payment_status", payment.PaymentStatus.String),
			slog.String("payment_success", payment.Success.String),
			slog.Any("err", err))
		if payment.Success.String != "1" {
			stats.failedSum.Inc(payment.Currency.String, payment.Amount.Float64)
		}
	} else {
		utils.LogFor(ctx).Error("Post-payment error - no payment details available",
			slog.String("terminal", terminal),
			slog.Any("err", err))
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetExtra("terminal", terminal)
		scope.SetExtra("error_type", "post_payment")
		if payment != nil {
			scope.SetExtra("payment_status", payment.PaymentStatus.String)
			scope.SetExtra("payment_success", payment.Success.String)
		}
		hub.CaptureException(err)
	})
	stats.errorCount.Inc("post_payment", 1)
}

func recordDeclinedOrder(ctx context.Context, stats *chargeStats, lastPayment *repo.Payment) {
	if lastPayment != nil {
		stats.failedSum.Inc(lastPayment.Currency.String, lastPayment.Amount.Float64)
	}
	stats.failedCount.Inc("total", 1)
	utils.LogFor(ctx).Info("Order declined on all terminals")
}

// getDefaultMaxWorkers returns the default number of workers for concurrent processing.
// Uses RENEWAL_MAX_WORKERS from envconfig, defaults to 5.
func getDefaultMaxWorkers() int {
	if common.Config.RenewalMaxWorkers > 0 {
		return common.Config.RenewalMaxWorkers
	}
	return 5
}
