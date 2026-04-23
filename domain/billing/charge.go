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
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// resolvedOrder holds pre-resolved data for a single order, ready for charging.
type resolvedOrder struct {
	OrderID uint
	Data    *repo.RenewalData
	Price   *pricing.ChargePrice
}

// chargeWithPricing runs the two-phase charge process:
// Phase 1 (pre-resolution): load data + resolve pricing for all orders.
// Phase 2 (charge): process resolved orders concurrently with terminal fallback.
func (s *BillingService) chargeWithPricing(ctx context.Context, maxWorkers int) (int, error) {
	log := utils.LogFor(ctx)

	orderIDs, err := s.repo.GetOrderIDsToRenew(ctx)
	if err != nil {
		return 0, fmt.Errorf("GetOrderIDsToRenew: %w", err)
	}
	if len(orderIDs) == 0 {
		log.Info("No orders to renew")
		return 0, nil
	}
	log.Info("Orders collected for renewal", slog.Int("total", len(orderIDs)))

	// Phase 1: Pre-resolve pricing (parallel)
	resolved, pricingErrors := s.preResolve(ctx, orderIDs, maxWorkers)
	v1Resolved, v2Resolved := 0, 0
	for _, ro := range resolved {
		if ro.Price.PricingVersion == "v2" {
			v2Resolved++
		} else {
			v1Resolved++
		}
	}
	log.Info("Pre-resolution complete",
		slog.Int("resolved", len(resolved)),
		slog.Int("v1_resolved", v1Resolved),
		slog.Int("v2_resolved", v2Resolved),
		slog.Int("pricing_errors", pricingErrors))

	if len(resolved) == 0 {
		log.Warn("All orders failed pricing resolution")
		return 0, nil
	}

	logDiscountStats(log, resolved)

	// Phase 2: Charge resolved orders concurrently
	return s.chargeResolved(ctx, resolved, maxWorkers)
}

// preResolve loads renewal data and resolves pricing for all orders concurrently.
// Orders that fail are flagged as pricing_error and excluded from charging.
func (s *BillingService) preResolve(ctx context.Context, orderIDs []uint, maxWorkers int) ([]resolvedOrder, int) {
	log := utils.LogFor(ctx)

	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	type preResolveResult struct {
		order resolvedOrder
		isErr bool
	}

	orderChan := make(chan uint, maxWorkers)
	resultChan := make(chan preResolveResult, len(orderIDs))

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for orderID := range orderChan {
				data, err := s.repo.LoadRenewalData(ctx, orderID)
				if err != nil {
					log.Error("Failed to load renewal data",
						slog.Uint64("order_id", uint64(orderID)),
						slog.Any("err", err))
					s.flagPricingError(ctx, orderID)
					resultChan <- preResolveResult{isErr: true}
					continue
				}

				price, err := s.resolver.Resolve(ctx, data.Account, data.Order.Currency.String)
				if err != nil {
					log.Error("Failed to resolve pricing",
						slog.Uint64("order_id", uint64(orderID)),
						slog.Int("account_id", data.Account.ID),
						slog.Any("err", err))
					s.flagPricingError(ctx, orderID)
					resultChan <- preResolveResult{isErr: true}
					continue
				}

				resultChan <- preResolveResult{order: resolvedOrder{
					OrderID: orderID,
					Data:    data,
					Price:   price,
				}}
			}
		}()
	}

	go func() {
		for _, id := range orderIDs {
			orderChan <- id
		}
		close(orderChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var resolved []resolvedOrder
	var pricingErrors int
	for r := range resultChan {
		if r.isErr {
			pricingErrors++
		} else {
			resolved = append(resolved, r.order)
		}
	}

	return resolved, pricingErrors
}

func (s *BillingService) flagPricingError(ctx context.Context, orderID uint) {
	if err := s.repo.FlagOrder(ctx, int(orderID), common.OrderFlagPricingError); err != nil {
		utils.LogFor(ctx).Error("Failed to flag pricing error",
			slog.Uint64("order_id", uint64(orderID)),
			slog.Any("err", err))
	}
}

// chargeResolved processes pre-resolved orders concurrently with terminal fallback (token → emv).
func (s *BillingService) chargeResolved(ctx context.Context, orders []resolvedOrder, maxWorkers int) (int, error) {
	if maxWorkers <= 0 {
		maxWorkers = getDefaultMaxWorkers()
	}

	log := utils.LogFor(ctx)
	log.Info("Starting charge phase", slog.Int("orders", len(orders)), slog.Int("workers", maxWorkers))

	stats := newChargeStats(int64(len(orders)))

	orderChan := make(chan resolvedOrder, maxWorkers)
	var wg sync.WaitGroup

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for ro := range orderChan {
				select {
				case <-ctx.Done():
					log.Info("Context cancelled, worker exiting", slog.Int("worker_id", workerID))
					return
				default:
					s.processWithRecovery(ctx, workerID, ro, &stats)
				}
			}
		}(i)
	}

dispatchLoop:
	for _, ro := range orders {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, stopping dispatch")
			break dispatchLoop
		case orderChan <- ro:
		}
	}
	close(orderChan)
	wg.Wait()

	successCount := stats.successCount.Get(pelecard.TokenTerminal.Name) + stats.successCount.Get(pelecard.EMVTerminal.Name)
	log.Info("Charge phase completed",
		// --- Overall counts ---
		slog.Int64("total_orders", stats.totalOrders),
		slog.Int64("successful", successCount),
		slog.Int64("via_token", stats.successCount.Get(pelecard.TokenTerminal.Name)),
		slog.Int64("via_emv", stats.successCount.Get(pelecard.EMVTerminal.Name)),
		slog.Float64("success_nis", stats.successSum.Get(common.CurrencyNIS)),
		slog.Float64("success_usd", stats.successSum.Get(common.CurrencyUSD)),
		slog.Float64("success_eur", stats.successSum.Get(common.CurrencyEUR)),
		slog.Int64("declined", stats.failedCount.Get("total")),
		slog.Float64("failed_nis", stats.failedSum.Get(common.CurrencyNIS)),
		slog.Float64("failed_usd", stats.failedSum.Get(common.CurrencyUSD)),
		slog.Float64("failed_eur", stats.failedSum.Get(common.CurrencyEUR)),
		slog.Int64("pre_payment_errors", stats.errorCount.Get("pre_payment")),
		slog.Int64("post_payment_errors", stats.errorCount.Get("post_payment")),
		slog.Int64("charge_success_db_fail", stats.errorCount.Get("charge_success_db_fail")),
		slog.Int64("gateway_errors", stats.errorCount.Get("gateway")),
		slog.Int64("panics", stats.errorCount.Get("panic")),
		// --- Per pricing version ---
		slog.Int64("v1_orders", stats.pricingVersionCount.Get("v1")),
		slog.Int64("v2_orders", stats.pricingVersionCount.Get("v2")),
		slog.Int64("v2_discounted", stats.v2DiscountCount.Get("eligible")),
		slog.Float64("v1_success_nis", stats.versionSuccessSum.Get("v1:"+common.CurrencyNIS)),
		slog.Float64("v1_success_usd", stats.versionSuccessSum.Get("v1:"+common.CurrencyUSD)),
		slog.Float64("v1_success_eur", stats.versionSuccessSum.Get("v1:"+common.CurrencyEUR)),
		slog.Float64("v2_success_nis", stats.versionSuccessSum.Get("v2:"+common.CurrencyNIS)),
		slog.Float64("v2_success_usd", stats.versionSuccessSum.Get("v2:"+common.CurrencyUSD)),
		slog.Float64("v2_success_eur", stats.versionSuccessSum.Get("v2:"+common.CurrencyEUR)),
		slog.Float64("v1_failed_nis", stats.versionFailedSum.Get("v1:"+common.CurrencyNIS)),
		slog.Float64("v1_failed_usd", stats.versionFailedSum.Get("v1:"+common.CurrencyUSD)),
		slog.Float64("v1_failed_eur", stats.versionFailedSum.Get("v1:"+common.CurrencyEUR)),
		slog.Float64("v2_failed_nis", stats.versionFailedSum.Get("v2:"+common.CurrencyNIS)),
		slog.Float64("v2_failed_usd", stats.versionFailedSum.Get("v2:"+common.CurrencyUSD)),
		slog.Float64("v2_failed_eur", stats.versionFailedSum.Get("v2:"+common.CurrencyEUR)),
		// --- Failure reason breakdown (amounts, cross-version) ---
		slog.Float64("declined_nis", stats.reasonFailedSum.Get("declined:"+common.CurrencyNIS)),
		slog.Float64("declined_usd", stats.reasonFailedSum.Get("declined:"+common.CurrencyUSD)),
		slog.Float64("declined_eur", stats.reasonFailedSum.Get("declined:"+common.CurrencyEUR)),
		slog.Float64("gateway_failed_nis", stats.reasonFailedSum.Get("gateway:"+common.CurrencyNIS)),
		slog.Float64("gateway_failed_usd", stats.reasonFailedSum.Get("gateway:"+common.CurrencyUSD)),
		slog.Float64("gateway_failed_eur", stats.reasonFailedSum.Get("gateway:"+common.CurrencyEUR)),
		slog.Float64("post_payment_failed_nis", stats.reasonFailedSum.Get("post_payment:"+common.CurrencyNIS)),
		slog.Float64("post_payment_failed_usd", stats.reasonFailedSum.Get("post_payment:"+common.CurrencyUSD)),
		slog.Float64("post_payment_failed_eur", stats.reasonFailedSum.Get("post_payment:"+common.CurrencyEUR)))

	return int(successCount), nil
}

// processWithRecovery processes a single resolved order with panic recovery and terminal fallback.
func (s *BillingService) processWithRecovery(ctx context.Context, workerID int, ro resolvedOrder, stats *chargeStats) {
	ctx = context.WithValue(ctx, common.CtxLogger, utils.LogFor(ctx).With(
		slog.Int("worker_id", workerID),
		slog.Uint64("order_id", uint64(ro.OrderID))))

	hub := utils.SentryFor(ctx).Clone()
	hub.Scope().SetExtra("worker_id", workerID)
	hub.Scope().SetExtra("order_id", ro.OrderID)
	ctx = sentry.SetHubOnContext(ctx, hub)

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			utils.LogFor(ctx).Error("Panic in worker",
				slog.Any("panic", r),
				slog.String("stack", stack))
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetExtra("stack", stack)
				hub.CaptureException(fmt.Errorf("panic in order renewal: %v", r))
			})
			stats.errorCount.Inc("panic", 1)
		}
	}()

	log := utils.LogFor(ctx)

	// Track pricing version and v2-specific discount stats up front.
	stats.pricingVersionCount.Inc(ro.Price.PricingVersion, 1)
	if ro.Price.PricingVersion == "v2" && ro.Price.V2Evaluation != nil {
		for _, d := range ro.Price.V2Evaluation.Discounts {
			if d.Type == pricing.DiscountTypeDonations {
				if d.Eligible {
					stats.v2DiscountCount.Inc("eligible", 1)
				}
				break
			}
		}
	}

	// === Token Terminal ===
	tokenPayment, tokenErr := processOrder(ctx, s.repo, s.eventEmitter, s.chargeExecutor, ro.Data, ro.Price, pelecard.TokenTerminal)
	if tokenErr == nil {
		if tokenPayment != nil && tokenPayment.Success.String == "1" {
			stats.successCount.Inc(pelecard.TokenTerminal.Name, 1)
			stats.successSum.Inc(ro.Price.Currency, ro.Price.Amount)
			stats.versionSuccessSum.Inc(ro.Price.PricingVersion+":"+ro.Price.Currency, ro.Price.Amount)
			log.Info("Order renewed successfully", slog.String("terminal", pelecard.TokenTerminal.Name))
			return
		}
		// Token declined — fall through to EMV
	} else if handleNonRetryableError(ctx, hub, stats, pelecard.TokenTerminal.Name, tokenPayment, tokenErr) {
		return
	} else {
		log.Error("Token terminal gateway error, falling back to EMV", slog.Any("err", tokenErr))
		captureError(hub, pelecard.TokenTerminal.Name, "gateway", tokenErr)
	}

	// === EMV Terminal (fallback) ===
	emvPayment, emvErr := processOrder(ctx, s.repo, s.eventEmitter, s.chargeExecutor, ro.Data, ro.Price, pelecard.EMVTerminal)
	if emvErr == nil {
		if emvPayment != nil && emvPayment.Success.String == "1" {
			stats.successCount.Inc(pelecard.EMVTerminal.Name, 1)
			stats.successSum.Inc(ro.Price.Currency, ro.Price.Amount)
			stats.versionSuccessSum.Inc(ro.Price.PricingVersion+":"+ro.Price.Currency, ro.Price.Amount)
			log.Info("Order renewed successfully", slog.String("terminal", pelecard.EMVTerminal.Name))
			return
		}
		recordDeclinedOrder(ctx, stats, ro.Price)
		return
	}

	if handleNonRetryableError(ctx, hub, stats, pelecard.EMVTerminal.Name, emvPayment, emvErr) {
		return
	}

	// Gateway error on both terminals
	log.Error("EMV terminal gateway error, all terminals exhausted", slog.Any("err", emvErr))
	captureError(hub, pelecard.EMVTerminal.Name, "gateway", emvErr)
	stats.errorCount.Inc("gateway", 1)
	stats.failedSum.Inc(ro.Price.Currency, ro.Price.Amount)
	stats.versionFailedSum.Inc(ro.Price.PricingVersion+":"+ro.Price.Currency, ro.Price.Amount)
	stats.reasonFailedSum.Inc("gateway:"+ro.Price.Currency, ro.Price.Amount)
}

func newChargeStats(totalOrders int64) chargeStats {
	return chargeStats{
		totalOrders:         totalOrders,
		successCount:        utils.NewCounterMap[int64](),
		successSum:          utils.NewCounterMap[float64](),
		failedCount:         utils.NewCounterMap[int64](),
		failedSum:           utils.NewCounterMap[float64](),
		errorCount:          utils.NewCounterMap[int64](),
		pricingVersionCount: utils.NewCounterMap[int64](),
		v2DiscountCount:     utils.NewCounterMap[int64](),
		versionSuccessSum:   utils.NewCounterMap[float64](),
		versionFailedSum:    utils.NewCounterMap[float64](),
		reasonFailedSum:     utils.NewCounterMap[float64](),
	}
}

// chargeStats tracks renewal statistics (thread-safe via CounterMap)
type chargeStats struct {
	totalOrders  int64
	successCount *utils.CounterMap[int64]
	successSum   *utils.CounterMap[float64]
	failedCount  *utils.CounterMap[int64]
	failedSum    *utils.CounterMap[float64]
	errorCount   *utils.CounterMap[int64]

	// Pricing version breakdown
	pricingVersionCount *utils.CounterMap[int64]   // "v1", "v2"
	v2DiscountCount     *utils.CounterMap[int64]   // "eligible"
	versionSuccessSum   *utils.CounterMap[float64] // "v1:NIS", "v2:USD", etc.
	versionFailedSum    *utils.CounterMap[float64] // total not-collected per version+currency
	reasonFailedSum     *utils.CounterMap[float64] // "declined:NIS", "gateway:USD", "post_payment:EUR", etc.
}

func handleNonRetryableError(ctx context.Context, hub *sentry.Hub, stats *chargeStats, terminal string, payment *repo.Payment, err error) bool {
	if errors.Is(err, common.ErrPrePayment) {
		utils.LogFor(ctx).Error("Pre-payment error",
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

func recordPostPaymentError(ctx context.Context, hub *sentry.Hub, stats *chargeStats, terminal string, payment *repo.Payment, err error) {
	if payment != nil {
		utils.LogFor(ctx).Error("Post-payment error",
			slog.String("terminal", terminal),
			slog.String("payment_status", payment.PaymentStatus.String),
			slog.String("payment_success", payment.Success.String),
			slog.Any("err", err))
		if payment.Success.String == "1" {
			stats.errorCount.Inc("charge_success_db_fail", 1)
		} else {
			stats.failedSum.Inc(payment.Currency.String, payment.Amount.Float64)
			stats.versionFailedSum.Inc(payment.PricingVersion.String+":"+payment.Currency.String, payment.Amount.Float64)
			stats.reasonFailedSum.Inc("post_payment:"+payment.Currency.String, payment.Amount.Float64)
		}
	} else {
		utils.LogFor(ctx).Error("Post-payment error - no payment details",
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

func recordDeclinedOrder(ctx context.Context, stats *chargeStats, price *pricing.ChargePrice) {
	stats.failedSum.Inc(price.Currency, price.Amount)
	stats.failedCount.Inc("total", 1)
	stats.versionFailedSum.Inc(price.PricingVersion+":"+price.Currency, price.Amount)
	stats.reasonFailedSum.Inc("declined:"+price.Currency, price.Amount)
	utils.LogFor(ctx).Info("Order declined on all terminals")
}

// logDiscountStats logs discount statistics for all resolved orders, grouped by discount type.
// Each log line represents one discount type and shows how much we are giving away vs. the base price.
// Only v2 orders have discount evaluations; v1 orders are skipped.
func logDiscountStats(log *slog.Logger, orders []resolvedOrder) {
	type discountTypeStat struct {
		eligibleOrders   int
		ineligibleOrders int
		amountByCurrency map[string]float64
	}

	byType := make(map[pricing.DiscountType]*discountTypeStat)

	for _, ro := range orders {
		if ro.Price.V2Evaluation == nil {
			continue
		}
		eval := ro.Price.V2Evaluation
		for _, d := range eval.Discounts {
			stat, ok := byType[d.Type]
			if !ok {
				stat = &discountTypeStat{amountByCurrency: make(map[string]float64)}
				byType[d.Type] = stat
			}
			if d.Eligible {
				stat.eligibleOrders++
				stat.amountByCurrency[eval.FinalPrice.Currency] += eval.CountryBase.Amount - eval.FinalPrice.Amount
			} else {
				stat.ineligibleOrders++
			}
		}
	}

	for discountType, stat := range byType {
		log.Info("Pre-charge discount statistics",
			slog.String("discount_type", string(discountType)),
			slog.Int("eligible_orders", stat.eligibleOrders),
			slog.Int("ineligible_orders", stat.ineligibleOrders),
			slog.Float64("discount_nis", stat.amountByCurrency[common.CurrencyNIS]),
			slog.Float64("discount_usd", stat.amountByCurrency[common.CurrencyUSD]),
			slog.Float64("discount_eur", stat.amountByCurrency[common.CurrencyEUR]))
	}
}

func getDefaultMaxWorkers() int {
	if common.Config.RenewalMaxWorkers > 0 {
		return common.Config.RenewalMaxWorkers
	}
	return 5
}
