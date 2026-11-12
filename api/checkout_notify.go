package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

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
// payment request turns valid. No Keycloak identity: the body is two ids and
// nothing in it is trusted. The facts come from asking checkout back, with
// this service's bearer. external_payments signs each notice with a shared
// HMAC key (see verifyNotifySignature); with CHECKOUT_NOTIFY_HMAC_ENFORCE a
// notice without a valid signature is refused before any lookup.
//
// The status tells checkout whether to retry: 200 done (or done before), 4xx
// other than 409 never, 409 and 5xx later. The bodies say nothing more: the
// route is public and the ids are sequential, so an answer that told paid from
// unknown from mismatched would let anyone map orders and payments. The
// details go to the log.
func (o *OrdersAPI) handleCheckoutNotify(c *gin.Context) {
	// The signature covers the exact bytes, so read them before binding.
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxNotifyBody))
	if err != nil {
		c.JSON(http.StatusBadRequest, notifyRefused)
		return
	}
	if err := verifyNotifySignature(raw, c.GetHeader("X-Notify-Timestamp"), c.GetHeader("X-Notify-Signature"),
		common.Config.CheckoutNotifyHMACKeys, time.Now()); err != nil {
		log := utils.LogFor(c.Request.Context()).With(slog.Any("err", err))
		if common.Config.CheckoutNotifyHMACEnforce {
			log.Warn("checkout notify refused: signature")
			c.JSON(http.StatusUnauthorized, notifyRefused)
			return
		}
		log.Warn("checkout notify: signature would be refused")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))

	var req checkoutNotification
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, notifyRefused)
		return
	}

	// Not cancelled with the caller: a confirm half-written because checkout
	// hung up is worse than a late one.
	ctx := context.WithoutCancel(c.Request.Context())
	alreadyPaid, err := o.confirmCheckoutPayment(ctx, req.UserKey, req.Reference)
	if err != nil {
		o.respondConfirmError(c, ctx, req, err)
		return
	}

	status := "paid"
	if alreadyPaid {
		status = "already_paid"
	}
	utils.LogFor(ctx).Info("checkout notify", slog.String("user_key", req.UserKey),
		slog.String("reference", req.Reference), slog.String("status", status))
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// maxNotifyBody bounds what an unauthenticated caller can make us read. A
// notice is two short ids.
const maxNotifyBody = 4 << 10

// notifyMaxSkew is how far a signed timestamp may be from now. Retries are
// signed afresh, so only a replayed request is ever this old.
const notifyMaxSkew = 5 * time.Minute

var (
	errNotifyNoKey     = errors.New("no CHECKOUT_NOTIFY_HMAC_KEY configured")
	errNotifyUnsigned  = errors.New("no signature")
	errNotifyStale     = errors.New("signature timestamp too old or in the future")
	errNotifyBadFormat = errors.New("signature not in the v1=<hex> form")
	errNotifyMismatch  = errors.New("signature does not match")
)

// verifyNotifySignature checks external_payments' signature on a notice:
// X-Notify-Signature is v1=hex(HMAC-SHA256(key, X-Notify-Timestamp + "." +
// body)) under any of keys, and the timestamp is within notifyMaxSkew of now.
func verifyNotifySignature(body []byte, ts, sig string, keys []string, now time.Time) error {
	if len(keys) == 0 {
		return errNotifyNoKey
	}
	if ts == "" || sig == "" {
		return errNotifyUnsigned
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errNotifyStale
	}
	if d := now.Sub(time.Unix(secs, 0)); d > notifyMaxSkew || d < -notifyMaxSkew {
		return errNotifyStale
	}
	hexSig, ok := strings.CutPrefix(sig, "v1=")
	got, err := hex.DecodeString(hexSig)
	if !ok || err != nil {
		return errNotifyBadFormat
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		if hmac.Equal(got, mac.Sum(nil)) {
			return nil
		}
	}
	return errNotifyMismatch
}

// notifyRefused is the one body every 4xx answer carries.
var notifyRefused = gin.H{"error": "cannot confirm this payment"}

func (o *OrdersAPI) respondConfirmError(c *gin.Context, ctx context.Context, req checkoutNotification, err error) {
	log := utils.LogFor(ctx).With(slog.String("user_key", req.UserKey),
		slog.String("reference", req.Reference), slog.Any("err", err))

	switch {
	case errors.Is(err, common.ErrInvalidValues), errors.Is(err, errNoSuchPayment):
		log.Warn("checkout notify for an unknown payment")
		c.JSON(http.StatusUnprocessableEntity, notifyRefused)
	case errors.Is(err, errGatewayMismatch):
		// Warn, not Sentry: anyone can post a pair that reaches this, and it
		// must not page anyone per call.
		log.Warn("checkout notify: checkout's record does not match")
		c.JSON(http.StatusUnprocessableEntity, notifyRefused)
	case errors.Is(err, pelecard.ErrPaymentNotFound), errors.Is(err, errNoTokenYet):
		log.Warn("checkout notify: nothing to confirm yet")
		c.JSON(http.StatusConflict, notifyRefused)
	case errors.Is(err, pelecard.ErrUnauthorized), errors.Is(err, pelecard.ErrNoCredential):
		c.Status(http.StatusBadGateway)
		_ = c.Error(fmt.Errorf("o.confirmCheckoutPayment: %w", err))
	default:
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("o.confirmCheckoutPayment: %w", err))
	}
}

// confirmCheckoutPayment marks one payment paid from what checkout recorded,
// never from what a caller says. A payment already marked successful is not
// rewritten and is reported as alreadyPaid, so a repeated notify or a late
// browser return changes none of checkout's facts; only an order left unsettled
// by an earlier, half-finished confirm is finished.
func (o *OrdersAPI) confirmCheckoutPayment(ctx context.Context, orderKey, paramX string) (alreadyPaid bool, err error) {
	payment, err := o.paymentOfOrder(ctx, orderKey, paramX)
	if err != nil {
		return false, err
	}
	orderID := payment.OrderID.Int
	if payment.PaymentStatus.String == common.PaymentStatusSuccess {
		if err := o.settleOrder(ctx, payment); err != nil {
			return false, err
		}
		return true, nil
	}

	facts, err := o.pages.GetPayment(ctx, orderKey, paramX)
	if err != nil {
		return false, fmt.Errorf("o.pages.GetPayment: %w", err)
	}
	if facts.UserKey != orderKey || facts.AdditionalDetailsParamX != paramX {
		return false, fmt.Errorf("%w: checkout has user_key %q, reference %q", errGatewayMismatch,
			facts.UserKey, facts.AdditionalDetailsParamX)
	}

	order, err := o.repo.GetOrderByID(ctx, uint(orderID))
	if err != nil {
		return false, fmt.Errorf("o.repo.GetOrderByID: %w", err)
	}
	// Renewals charge with both, so a recurring order needs both.
	if order.Type.String == common.OrderTypeRecurring && (facts.Token == "" || facts.ApprovalNo == "") {
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

// paymentOfOrder reads the payment paramX names, and refuses it unless it
// belongs to the order orderKey names. Every lookup of a payment from a
// caller's ids goes through here: a caller is checked against the order in
// user_key, so a payment found from the reference alone could be another
// member's.
func (o *OrdersAPI) paymentOfOrder(ctx context.Context, orderKey, paramX string) (*repo.Payment, error) {
	orderID, err := keyID(orderKey, "ord")
	if err != nil {
		return nil, err
	}
	paymentID, err := keyID(paramX, "m")
	if err != nil {
		return nil, err
	}
	payment, err := o.repo.GetPaymentByID(ctx, paymentID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && payment.OrderID.Int != orderID) {
		return nil, fmt.Errorf("%w: payment %d, order %d", errNoSuchPayment, paymentID, orderID)
	}
	if err != nil {
		return nil, fmt.Errorf("o.repo.GetPaymentByID: %w", err)
	}
	return payment, nil
}

// paidPayment returns the payment paramX names when it belongs to orderKey's
// order and is marked successful, else nil. Ids it cannot parse, or that name
// no payment of that order, are not paid, and the caller's own checks report
// them. A failed read is an error, not "not paid": the caller would go on to
// write the browser's facts over a payment checkout may already have
// confirmed.
func (o *OrdersAPI) paidPayment(ctx context.Context, orderKey, paramX string) (*repo.Payment, error) {
	payment, err := o.paymentOfOrder(ctx, orderKey, paramX)
	if errors.Is(err, common.ErrInvalidValues) || errors.Is(err, errNoSuchPayment) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if payment.PaymentStatus.String != common.PaymentStatusSuccess {
		return nil, nil
	}
	return payment, nil
}

// settleOrder finishes the order of a successful payment when an earlier
// confirm wrote the payment and then failed before the order: without it, the
// retry stops at "already paid" and the order stays new although the member
// paid. An order with a PaymentDate was settled and is left alone.
func (o *OrdersAPI) settleOrder(ctx context.Context, payment *repo.Payment) error {
	if payment.Success.String != "1" {
		return nil
	}
	order, err := o.repo.GetOrderByID(ctx, uint(payment.OrderID.Int))
	if err != nil {
		return fmt.Errorf("o.repo.GetOrderByID: %w", err)
	}
	if order.PaymentDate.Valid {
		return nil
	}
	if err := o.repo.UpdateOrderAfterPayment(ctx, *payment); err != nil {
		return fmt.Errorf("o.repo.UpdateOrderAfterPayment: %w", err)
	}
	return nil
}

// keyID reads the number out of an id in exactly the form this service builds
// it: "ord-123" or "m-45" followed by this environment's SUFX ("-G1" on dev).
// An id with another environment's suffix, or anything after the number, is
// refused, so a dev pair cannot confirm a prod payment with the same numbers.
// The number is plain decimal digits as FormatUint writes them: no sign, no
// leading zero. Ids are int4 serials, so it must fit in 31 bits and be
// positive.
func keyID(key, prefix string) (int, error) {
	suffix := os.Getenv("SUFX")
	n, okPrefix := strings.CutPrefix(key, prefix+"-")
	n, okSuffix := strings.CutSuffix(n, suffix)
	digits := n != "" && n[0] != '0' && strings.Trim(n, "0123456789") == ""
	id, err := strconv.ParseInt(n, 10, 32)
	if !okPrefix || !okSuffix || !digits || err != nil {
		return 0, fmt.Errorf("%w: %q is not a %s-<id>%s id", common.ErrInvalidValues, key, prefix, suffix)
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
		TransactionUpdateTime: str(facts.TransactionUpdateTime),
		VoucherID:             str(facts.VoucherId),
	}
}
