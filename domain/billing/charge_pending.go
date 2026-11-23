package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/pelecard"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
)

// The statuses external_payments reports for a charge request. "new" and
// "in-process" are not settled yet and are left for a later run.
const (
	chargeRequestValid   = "valid"
	chargeRequestInvalid = "invalid"
	chargeRequestCancel  = "cancel"
	chargeRequestError   = "error"
)

// ReconcilePendingResult counts what one reconcile-pending run did.
type ReconcilePendingResult struct {
	Checked   int
	Charged   int
	NotCharge int
	Unsettled int
	Errors    int
}

// ReconcilePendingCharges settles the renewals held charge_pending: charges that
// got no clear answer, which external_payments reconciles against Pelecard on
// its own. Only payments created before now-olderThan are asked about, so that
// reconciliation has had its time.
//
// valid → the payment succeeds and the order is renewed, as if the charge had
// answered success. invalid, cancel, error, or no such request → the payment
// fails and the order goes back to torenew for the next run of billing. Anything
// else is not settled yet and stays held. With dryRun nothing is written.
func (s *BillingService) ReconcilePendingCharges(ctx context.Context, olderThan time.Duration, dryRun bool) (ReconcilePendingResult, error) {
	var result ReconcilePendingResult
	pending, err := s.repo.GetChargePendingRenewals(ctx, time.Now().Add(-olderThan))
	if err != nil {
		return result, fmt.Errorf("GetChargePendingRenewals: %w", err)
	}

	for _, pc := range pending {
		result.Checked++
		log := utils.LogFor(ctx).With(slog.Int("order_id", int(pc.OrderID)), slog.Int("payment_id", pc.PaymentID))
		charged, settled, err := s.settlePendingCharge(ctx, pc.OrderID, pc.PaymentID, dryRun)
		switch {
		case err != nil:
			result.Errors++
			log.Error("reconcile-pending: could not settle", slog.Any("err", err))
		case !settled:
			result.Unsettled++
			log.Info("reconcile-pending: not settled yet")
		case charged:
			result.Charged++
			log.Info("reconcile-pending: charged", slog.Bool("dry_run", dryRun))
		default:
			result.NotCharge++
			log.Info("reconcile-pending: not charged", slog.Bool("dry_run", dryRun))
		}
	}

	utils.LogFor(ctx).Info("reconcile-pending completed",
		slog.Int("checked", result.Checked), slog.Int("charged", result.Charged),
		slog.Int("not_charged", result.NotCharge), slog.Int("unsettled", result.Unsettled),
		slog.Int("errors", result.Errors), slog.Bool("dry_run", dryRun))
	return result, nil
}

// settlePendingCharge asks external_payments about one held payment and, once
// it knows, finalizes the renewal the way processOrder would have.
func (s *BillingService) settlePendingCharge(ctx context.Context, orderID uint, paymentID int, dryRun bool) (charged, settled bool, err error) {
	payment, err := s.repo.GetPaymentByID(ctx, paymentID)
	if err != nil {
		return false, false, fmt.Errorf("GetPaymentByID: %w", err)
	}

	status, err := s.pelecardClient.GetChargeStatus(ctx, payment.Ordkey.String, payment.ParamX.String)
	switch {
	case errors.Is(err, pelecard.ErrPaymentNotFound):
		// external_payments never stored the request, so it never charged it.
		status = chargeRequestInvalid
	case err != nil:
		return false, false, fmt.Errorf("GetChargeStatus: %w", err)
	}

	switch status {
	case chargeRequestValid:
		charged = true
	case chargeRequestInvalid, chargeRequestCancel, chargeRequestError:
		charged = false
	default:
		return false, false, nil
	}
	if dryRun {
		return charged, true, nil
	}

	if charged {
		payment.PaymentStatus = null.StringFrom(common.PaymentStatusSuccess)
		payment.Success = null.StringFrom("1")
	} else {
		payment.PaymentStatus = null.StringFrom(common.PaymentStatusFailed)
		payment.Success = null.StringFrom("0")
	}
	if err := s.repo.FinalizeRenewal(ctx, orderID, payment); err != nil {
		return charged, false, fmt.Errorf("FinalizeRenewal: %w", err)
	}
	emitOrderEvent(ctx, s.eventEmitter, int(orderID))
	return charged, true, nil
}
