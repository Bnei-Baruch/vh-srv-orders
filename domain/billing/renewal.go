package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// BuildChargeRequest creates a pelecard.ChargeRequest from renewal data and resolved price.
func BuildChargeRequest(data *repo.RenewalData, price *pricing.ChargePrice, payment *repo.Payment) *pelecard.ChargeRequest {
	return &pelecard.ChargeRequest{
		UserKey: payment.Ordkey.String,

		GoodURL:    "http://ec41a043fda1.ngrok.io/pelecard/good",
		ErrorURL:   "http://ec41a043fda1.ngrok.io/pelecard/error",
		CancelURL:  "http://ec41a043fda1.ngrok.io/pelecard/cancel",
		ApprovalNo: payment.AuthNo.String,
		Token:      payment.PelecardToken.String,

		Name:         utils.StripNonBMP(data.Account.FirstName.String + " " + data.Account.LastName.String),
		Price:        price.Amount,
		Currency:     price.Currency,
		Email:        data.Account.Email.String,
		Phone:        "+NA",
		Street:       utils.StripNonBMP(data.Account.Street.String),
		City:         utils.StripNonBMP(data.Account.City.String),
		Country:      "Undef",
		Participans:  "1",
		Details:      "Membership",
		SKU:          "40037",
		VAT:          "f",
		Installments: 1,
		Language:     data.Order.OrderLanguage.String,
		Reference:    payment.ParamX.String,
		Organization: "ben2",
	}
}

// processOrder handles a single order renewal: creates payment, charges via gateway, finalizes in DB.
// Uses a non-cancellable context for payment operations to prevent state corruption.
func processOrder(
	ctx context.Context,
	ordersRepo repo.OrdersRepository,
	eventEmitter events.EventEmitter,
	chargeExecutor pelecard.ChargeExecutor,
	data *repo.RenewalData,
	price *pricing.ChargePrice,
	terminal pelecard.Terminal,
) (*repo.Payment, error) {
	log := utils.LogFor(ctx)

	// Marshal V2 evaluation for DB storage
	var pricingEvalJSON null.JSON
	if price.V2Evaluation != nil {
		raw, err := json.Marshal(price.V2Evaluation)
		if err != nil {
			return nil, fmt.Errorf("json.Marshal pricing evaluation: %w", err)
		}
		pricingEvalJSON = null.JSONFrom(raw)
	}

	// Create pending payment with resolved price
	payment, err := ordersRepo.CreateRenewalPayment(
		ctx, data, price.Amount, price.Currency,
		price.PricingVersion, pricingEvalJSON, terminal.PMX,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateRenewalPayment: %w", err)
	}
	payment.Terminal = null.StringFrom(terminal.Name)

	chargeReq := BuildChargeRequest(data, price, payment)

	// Execute charge (non-cancellable context to prevent payment state corruption)
	paymentCtx := context.WithoutCancel(ctx)
	resp, payErr := chargeExecutor.Execute(paymentCtx, chargeReq, terminal, uint(data.Order.ID))

	if payErr != nil {
		payment.PaymentStatus = null.StringFrom(common.PaymentStatusFailed)
		payment.Success = null.StringFrom("0")
	} else {
		status, _ := resp["status"].(string)
		if status == common.PaymentStatusSuccess {
			payment.PaymentStatus = null.StringFrom(common.PaymentStatusSuccess)
			payment.Success = null.StringFrom("1")
		} else {
			payment.PaymentStatus = null.StringFrom(common.PaymentStatusFailed)
			payment.Success = null.StringFrom("0")
			log.Info("payment declined",
				slog.String("terminal", terminal.Name),
				slog.Any("gateway_response", resp))
		}
	}

	// Finalize in DB (transaction: update payment + order)
	if finalizeErr := ordersRepo.FinalizeRenewal(ctx, uint(data.Order.ID), payment); finalizeErr != nil {
		if payment.Success.String == "1" {
			// CRITICAL: customer was charged but DB update failed.
			// Log with structured marker for reconciliation — contains all data needed
			// to reconstruct the missing DB writes from log consumption.
			log.Error("CHARGE_SUCCESS_DB_FAIL",
				slog.Int("order_id", data.Order.ID),
				slog.Int("account_id", data.Account.ID),
				slog.Int("payment_id", payment.ID),
				slog.Float64("amount", price.Amount),
				slog.String("currency", price.Currency),
				slog.String("pricing_version", price.PricingVersion),
				slog.String("terminal", terminal.Name),
				slog.Any("err", finalizeErr))
		}
		return payment, finalizeErr
	}

	// Emit event after successful DB commit
	emitOrderEvent(ctx, eventEmitter, data.Order.ID)

	if payErr != nil {
		return payment, fmt.Errorf("payment gateway error: %w", payErr)
	}

	return payment, nil
}

func emitOrderEvent(ctx context.Context, emitter events.EventEmitter, orderID int) {
	builder, ok := ctx.Value(common.CtxEventBuilder).(events.EventBuilder)
	if !ok || builder == nil {
		return
	}
	event := builder.BuildEvent(events.TypeUpdateOrder, map[string]interface{}{"order_id": orderID})
	emitter.Emit(ctx, event)
}
