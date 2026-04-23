package billing

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ReconcileEntry represents a single CHARGE_SUCCESS_DB_FAIL event parsed from logs.
type ReconcileEntry struct {
	OrderID        uint
	AccountID      int
	PaymentID      int
	Amount         float64
	Currency       string
	PricingVersion string
	Terminal       string
}

// ReconcileResult holds the outcome of a reconciliation run.
type ReconcileResult struct {
	Total             int
	Reconciled        int
	AlreadyReconciled int
	Failed            int
}

// Reconcile retries FinalizeRenewal for orders that were charged successfully but whose DB writes failed.
// Each entry is verified against current DB state before attempting reconciliation.
func (s *BillingService) Reconcile(ctx context.Context, entries []ReconcileEntry) ReconcileResult {
	log := utils.LogFor(ctx)
	var result ReconcileResult
	result.Total = len(entries)

	for _, entry := range entries {
		log.Info("Reconciling entry",
			slog.Int("payment_id", entry.PaymentID),
			slog.Uint64("order_id", uint64(entry.OrderID)),
			slog.Int("account_id", entry.AccountID))

		payment, err := s.repo.GetPaymentByID(ctx, entry.PaymentID)
		if err != nil {
			log.Error("Failed to load payment for reconciliation",
				slog.Int("payment_id", entry.PaymentID),
				slog.Any("err", err))
			result.Failed++
			continue
		}

		// Already reconciled — payment is already marked as success and order is renewed
		if payment.Success.String == "1" && payment.PaymentStatus.String == common.PaymentStatusSuccess {
			log.Info("Payment already reconciled",
				slog.Int("payment_id", entry.PaymentID))
			result.AlreadyReconciled++
			continue
		}

		// Cross-check entry against DB payment. A CHARGE_SUCCESS_DB_FAIL log entry is
		// only meaningful if every identifying field still matches what's in the DB;
		// a mismatch means the log is stale, reprocessed, or tampered and we must not
		// blindly finalize under those fields.
		if mismatch := verifyEntryMatchesPayment(entry, payment); mismatch != "" {
			log.Error("Reconcile entry does not match DB payment — refusing to finalize",
				slog.Int("payment_id", entry.PaymentID),
				slog.String("mismatch", mismatch))
			result.Failed++
			continue
		}

		// The only legitimate state for reconcile is pending. CHARGE_SUCCESS_DB_FAIL is
		// logged between CreateRenewalPayment (pending) and a failed FinalizeRenewal,
		// so any other status means the payment was already resolved — success (handled
		// above), declined (finalized as failed), or manually edited. Overwriting any
		// of those would corrupt financial state.
		if payment.PaymentStatus.String != common.PaymentStatusPending {
			log.Error("Reconcile: payment is not in pending state, refusing to overwrite",
				slog.Int("payment_id", entry.PaymentID),
				slog.String("payment_status", payment.PaymentStatus.String),
				slog.String("payment_success", payment.Success.String))
			result.Failed++
			continue
		}

		// Set the fields that processOrder would have set before FinalizeRenewal
		payment.Success = null.StringFrom("1")
		payment.PaymentStatus = null.StringFrom(common.PaymentStatusSuccess)
		payment.Terminal = null.StringFrom(entry.Terminal)

		if err := s.repo.FinalizeRenewal(ctx, entry.OrderID, payment); err != nil {
			log.Error("Reconciliation FinalizeRenewal failed",
				slog.Int("payment_id", entry.PaymentID),
				slog.Uint64("order_id", uint64(entry.OrderID)),
				slog.Any("err", err))
			result.Failed++
			continue
		}

		// Emit the event that the original processOrder would have emitted after FinalizeRenewal
		emitOrderEvent(ctx, s.eventEmitter, int(entry.OrderID))

		log.Info("Reconciled successfully",
			slog.Int("payment_id", entry.PaymentID),
			slog.Uint64("order_id", uint64(entry.OrderID)))
		result.Reconciled++
	}

	return result
}

// verifyEntryMatchesPayment cross-references the log entry against the DB payment.
// Returns an empty string when consistent, or a human-readable "; "-joined list of
// every mismatched field. Mismatches indicate a stale or wrong log and must never
// be silently finalized — the log's order_id/amount/currency/pricing_version are
// the gateway's view at charge time and should still match the DB row we're about
// to mutate.
func verifyEntryMatchesPayment(entry ReconcileEntry, payment *repo.Payment) string {
	var mismatches []string
	if int(payment.OrderID.Int) != int(entry.OrderID) {
		mismatches = append(mismatches, fmt.Sprintf("order_id (entry=%d, db=%d)", entry.OrderID, payment.OrderID.Int))
	}
	if payment.Amount.Float64 != entry.Amount {
		mismatches = append(mismatches, fmt.Sprintf("amount (entry=%v, db=%v)", entry.Amount, payment.Amount.Float64))
	}
	if payment.Currency.String != entry.Currency {
		mismatches = append(mismatches, fmt.Sprintf("currency (entry=%q, db=%q)", entry.Currency, payment.Currency.String))
	}
	if payment.PricingVersion.String != entry.PricingVersion {
		mismatches = append(mismatches, fmt.Sprintf("pricing_version (entry=%q, db=%q)", entry.PricingVersion, payment.PricingVersion.String))
	}
	return strings.Join(mismatches, "; ")
}

// slog TextHandler key=value pattern: handles key=value and key="quoted value"
var slogFieldPattern = regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|\S+)`)

// ParseReconcileEntries reads slog TextHandler log lines from r and extracts CHARGE_SUCCESS_DB_FAIL entries.
func ParseReconcileEntries(r io.Reader) ([]ReconcileEntry, error) {
	var entries []ReconcileEntry
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "CHARGE_SUCCESS_DB_FAIL") {
			continue
		}

		entry, err := parseLogLine(line)
		if err != nil {
			return nil, fmt.Errorf("parse log line: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return entries, nil
}

func parseLogLine(line string) (ReconcileEntry, error) {
	fields := make(map[string]string)
	for _, match := range slogFieldPattern.FindAllStringSubmatch(line, -1) {
		key := match[1]
		val := match[2]
		// Strip surrounding quotes
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		fields[key] = val
	}

	var entry ReconcileEntry
	var err error

	orderID, ok := fields["order_id"]
	if !ok {
		return entry, fmt.Errorf("missing order_id")
	}
	oid, err := strconv.ParseUint(orderID, 10, 64)
	if err != nil {
		return entry, fmt.Errorf("invalid order_id %q: %w", orderID, err)
	}
	entry.OrderID = uint(oid)

	accountID, ok := fields["account_id"]
	if !ok {
		return entry, fmt.Errorf("missing account_id")
	}
	entry.AccountID, err = strconv.Atoi(accountID)
	if err != nil {
		return entry, fmt.Errorf("invalid account_id %q: %w", accountID, err)
	}

	paymentID, ok := fields["payment_id"]
	if !ok {
		return entry, fmt.Errorf("missing payment_id")
	}
	entry.PaymentID, err = strconv.Atoi(paymentID)
	if err != nil {
		return entry, fmt.Errorf("invalid payment_id %q: %w", paymentID, err)
	}

	amount, ok := fields["amount"]
	if !ok {
		return entry, fmt.Errorf("missing amount")
	}
	entry.Amount, err = strconv.ParseFloat(amount, 64)
	if err != nil {
		return entry, fmt.Errorf("invalid amount %q: %w", amount, err)
	}

	entry.Currency = fields["currency"]
	entry.PricingVersion = fields["pricing_version"]
	entry.Terminal = fields["terminal"]

	return entry, nil
}
