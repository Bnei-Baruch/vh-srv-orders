package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

var (
	// errNoSuchPayment: the ids name no payment of that order here. Retrying
	// cannot help.
	errNoSuchPayment = errors.New("no such payment for that order")
	// errGatewayMismatch: checkout's record names a different order or
	// payment than the one asked about. Retrying cannot help.
	errGatewayMismatch = errors.New("checkout's payment does not match the order and reference")
	// errNoTokenYet: a recurring order whose payment checkout recorded without
	// the card token. Marking it paid would leave renewals nothing to charge.
	errNoTokenYet = errors.New("checkout has not stored the card token for this recurring order")
)

type checkoutNotification struct {
	UserKey   string `json:"user_key" binding:"required"`
	Reference string `json:"reference" binding:"required"`
}

// handleCheckoutNotify is called by external_payments, server to server, when a
// payment request turns valid. Unauthenticated by design: the body is two ids
// and nothing in it is trusted. The facts come from asking checkout back, with
// this service's bearer.
//
// The status tells checkout whether to retry: 200 done (or done before), 4xx
// other than 409 never, 409 and 5xx later.
func (o *OrdersAPI) handleCheckoutNotify(c *gin.Context) {
	var req checkoutNotification
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	alreadyPaid, err := o.confirmCheckoutPayment(ctx, req.UserKey, req.Reference)
	if err != nil {
		o.respondConfirmError(c, req, err)
		return
	}

	status := "paid"
	if alreadyPaid {
		status = "already_paid"
	}
	utils.LogFor(ctx).Info("checkout notify", slog.String("user_key", req.UserKey),
		slog.String("reference", req.Reference), slog.String("status", status))
	c.JSON(http.StatusOK, gin.H{"status": status})
}

func (o *OrdersAPI) respondConfirmError(c *gin.Context, req checkoutNotification, err error) {
	log := utils.LogFor(c.Request.Context()).With(slog.String("user_key", req.UserKey),
		slog.String("reference", req.Reference), slog.Any("err", err))

	switch {
	case errors.Is(err, common.ErrInvalidValues), errors.Is(err, errNoSuchPayment):
		log.Warn("checkout notify for an unknown payment")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, errGatewayMismatch):
		log.Error("checkout notify: checkout's record does not match")
		utils.SentryFor(c.Request.Context()).CaptureException(err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, pelecard.ErrPaymentNotFound), errors.Is(err, errNoTokenYet):
		log.Warn("checkout notify: nothing to confirm yet")
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, pelecard.ErrUnauthorized), errors.Is(err, pelecard.ErrNoCredential):
		c.Status(http.StatusBadGateway)
		_ = c.Error(fmt.Errorf("o.confirmCheckoutPayment: %w", err))
	default:
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("o.confirmCheckoutPayment: %w", err))
	}
}

// confirmCheckoutPayment marks one payment paid from what checkout recorded,
// never from what a caller says. A payment already marked successful is left
// alone and reported as alreadyPaid, so a repeated notify or a late browser
// return rewrites nothing.
func (o *OrdersAPI) confirmCheckoutPayment(ctx context.Context, orderKey, paramX string) (alreadyPaid bool, err error) {
	orderID, err := keyID(orderKey, "ord")
	if err != nil {
		return false, err
	}
	paymentID, err := keyID(paramX, "m")
	if err != nil {
		return false, err
	}

	payment, err := o.repo.GetPaymentByID(ctx, paymentID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && payment.OrderID.Int != orderID) {
		return false, fmt.Errorf("%w: payment %d, order %d", errNoSuchPayment, paymentID, orderID)
	}
	if err != nil {
		return false, fmt.Errorf("o.repo.GetPaymentByID: %w", err)
	}
	if payment.PaymentStatus.String == common.PaymentStatusSuccess {
		return true, nil
	}

	facts, err := o.checkout.GetPayment(ctx, orderKey)
	if err != nil {
		return false, fmt.Errorf("o.checkout.GetPayment: %w", err)
	}
	if facts.UserKey != orderKey || facts.AdditionalDetailsParamX != paramX {
		return false, fmt.Errorf("%w: checkout has user_key %q, reference %q", errGatewayMismatch,
			facts.UserKey, facts.AdditionalDetailsParamX)
	}

	order, err := o.repo.GetOrderByID(ctx, uint(orderID))
	if err != nil {
		return false, fmt.Errorf("o.repo.GetOrderByID: %w", err)
	}
	if order.Type.String == common.OrderTypeRecurring && facts.Token == "" {
		return false, errNoTokenYet
	}

	paid, err := o.repo.UpdatePayment(ctx, paidFromGateway(facts))
	if err != nil {
		return false, fmt.Errorf("o.repo.UpdatePayment: %w", err)
	}
	if err := o.repo.UpdateOrderAfterPayment(ctx, *paid); err != nil {
		return false, fmt.Errorf("o.repo.UpdateOrderAfterPayment: %w", err)
	}

	return false, nil
}

// alreadyPaid reports whether the payment paramX names is marked successful.
// Anything it cannot read counts as not paid, leaving the caller's own checks
// to report it.
func (o *OrdersAPI) alreadyPaid(ctx context.Context, paramX string) bool {
	paymentID, err := keyID(paramX, "m")
	if err != nil {
		return false
	}
	payment, err := o.repo.GetPaymentByID(ctx, paymentID)
	return err == nil && payment.PaymentStatus.String == common.PaymentStatusSuccess
}

// keyID reads the number out of an id like "ord-123" or "m-45", the way
// UpdatePayment does, refusing what it would panic on. Ids are int4 serials,
// so the number is parsed to 32 bits and must be positive.
func keyID(key, prefix string) (int, error) {
	parts := strings.Split(key, "-")
	if len(parts) < 2 || parts[0] != prefix {
		return 0, fmt.Errorf("%w: %q is not a %s- id", common.ErrInvalidValues, key, prefix)
	}
	id, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", common.ErrInvalidValues, key, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("%w: %q is not a positive id", common.ErrInvalidValues, key)
	}
	return int(id), nil
}

// paidFromGateway builds the successful-payment update from checkout's record
// only. Success is implied: checkout returns only requests that turned valid.
func paidFromGateway(facts *pelecard.GatewayPayment) repo.RequestPaid {
	str := func(s string) null.String { return null.NewString(s, s != "") }
	return repo.RequestPaid{
		UserKey:               str(facts.UserKey),
		ParamX:                str(facts.AdditionalDetailsParamX),
		Success:               null.StringFrom("1"),
		TransactionID:         str(facts.TransactionId),
		Token:                 str(facts.Token),
		AuthNo:                str(facts.ApprovalNo),
		CardHebrewName:        str(facts.CardHebrewName),
		CCAbroadCard:          str(facts.CreditCardAbroadCard),
		CCBrand:               str(facts.CreditCardBrand),
		CCCompanyClearer:      str(facts.CreditCardCompanyClearer),
		CCCompanyIssuer:       str(facts.CreditCardCompanyIssuer),
		CreditType:            str(facts.CreditType),
		CCNumber:              str(facts.CreditCardNumber),
		CCExpDate:             str(facts.CreditCardExpDate),
		DebitCode:             str(facts.DebitCode),
		DebitCurrency:         str(facts.DebitCurrency),
		DebitTotal:            str(facts.DebitTotal),
		DebitType:             str(facts.DebitType),
		FirstPaymentTotal:     str(facts.FirstPaymentTotal),
		FixedPaymentTotal:     str(facts.FixedPaymentTotal),
		TotalPayments:         str(facts.TotalPayments),
		JParam:                str(facts.JParam),
		TransactionInitTime:   str(facts.TransactionInitTime),
		TransactionPelecardID: str(facts.TransactionPelecardId),
		TransactionUpdateTime: str(facts.TransactionUpdateTime),
		VoucherID:             str(facts.VoucherId),
	}
}
