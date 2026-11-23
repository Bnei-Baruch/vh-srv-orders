package pelecard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
)

// ErrPaymentNotFound means external_payments has no completed payment for the
// user key under this service's organization: not paid yet, not paid at all,
// or not ours. external_payments answers all three alike, on purpose.
var ErrPaymentNotFound = errors.New("external_payments has no completed payment for that user_key")

// GatewayPayment is what external_payments recorded about one completed
// payment. Its JSON keys are the Go field names on that side, which has no
// json tags.
type GatewayPayment struct {
	UserKey                  string `json:"UserKey"`
	TransactionId            string `json:"TransactionId"`
	AdditionalDetailsParamX  string `json:"AdditionalDetailsParamX"`
	CardHebrewName           string `json:"CardHebrewName"`
	CreditCardAbroadCard     string `json:"CreditCardAbroadCard"`
	CreditCardBrand          string `json:"CreditCardBrand"`
	CreditCardCompanyClearer string `json:"CreditCardCompanyClearer"`
	CreditCardCompanyIssuer  string `json:"CreditCardCompanyIssuer"`
	CreditType               string `json:"CreditType"`
	CreditCardNumber         string `json:"CreditCardNumber"`
	CreditCardExpDate        string `json:"CreditCardExpDate"`
	DebitCode                string `json:"DebitCode"`
	DebitCurrency            string `json:"DebitCurrency"`
	DebitTotal               string `json:"DebitTotal"`
	DebitType                string `json:"DebitType"`
	FirstPaymentTotal        string `json:"FirstPaymentTotal"`
	FixedPaymentTotal        string `json:"FixedPaymentTotal"`
	TotalPayments            string `json:"TotalPayments"`
	JParam                   string `json:"JParam"`
	TransactionInitTime      string `json:"TransactionInitTime"`
	TransactionPelecardId    string `json:"TransactionPelecardId"`
	TransactionUpdateTime    string `json:"TransactionUpdateTime"`
	VoucherId                string `json:"VoucherId"`

	// The card token and approval number renewals charge with, as the Good
	// callback received them. Named apart from Pelecard's own keys on that
	// side. Empty for payments recorded before external_payments#17.
	Token      string `json:"CardToken"`
	ApprovalNo string `json:"CardApprovalNo"`
}

// paymentReadTimeout bounds one payment read. The client's own timeout is
// sized for charges, which wait on the card network; this is a database read.
// A variable only so a test can shorten it.
var paymentReadTimeout = 15 * time.Second

// GetPayment asks external_payments what the gateway said about one payment,
// so this service does not have to take the facts from the browser.
//
// The reference (the payment's ParamX) picks the payment: an order's user key
// is reused, by a member who pays twice or by renewals, and without it the
// newest payment of the order answers for all of them.
func (c *Client) GetPayment(ctx context.Context, userKey, reference string) (*GatewayPayment, error) {
	if c.Tokens == nil {
		return nil, fmt.Errorf("no token source configured for external_payments")
	}

	ctx, cancel := context.WithTimeout(ctx, paymentReadTimeout)
	defer cancel()

	resp, err := c.sendAuthorized(ctx, "payment", func(r *resty.Request) (*resty.Response, error) {
		return r.SetQueryParam("reference", reference).
			Get(c.BaseURL + "/vh/payment/" + url.PathEscape(userKey))
	})
	if err != nil {
		return nil, err
	}

	switch {
	// A 403 is a valid key that is not allowed on /vh: the same credential
	// fault as a 401, as on the charge and page routes.
	case resp.StatusCode() == http.StatusUnauthorized, resp.StatusCode() == http.StatusForbidden:
		return nil, fmt.Errorf("%w: payment (%d)", ErrUnauthorized, resp.StatusCode())
	case resp.StatusCode() == http.StatusNotFound:
		return nil, ErrPaymentNotFound
	case resp.IsError():
		return nil, utils.RespStatusError(resp, "external payment error")
	}

	var payment GatewayPayment
	if err := json.Unmarshal(resp.Body(), &payment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal external payment response: %w", err)
	}

	return &payment, nil
}

// GetChargeStatus asks external_payments how a charge it answered "pending"
// ended: the status of the request with this user key and reference — "valid"
// charged, "invalid" or "cancel" not charged, "new" or "in-process" not settled
// yet. ErrPaymentNotFound when external_payments has no such request.
func (c *Client) GetChargeStatus(ctx context.Context, userKey, reference string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, paymentReadTimeout)
	defer cancel()

	resp, err := c.sendAuthorized(ctx, "charge status", func(r *resty.Request) (*resty.Response, error) {
		return r.SetQueryParam("reference", reference).
			Get(c.BaseURL + "/vh/charge/" + url.PathEscape(userKey))
	})
	if err != nil {
		return "", err
	}

	switch {
	case resp.StatusCode() == http.StatusUnauthorized, resp.StatusCode() == http.StatusForbidden:
		return "", fmt.Errorf("%w: charge status (%d)", ErrUnauthorized, resp.StatusCode())
	case resp.StatusCode() == http.StatusNotFound:
		return "", ErrPaymentNotFound
	case resp.IsError():
		return "", utils.RespStatusError(resp, "external charge status error")
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return "", fmt.Errorf("failed to unmarshal external charge status: %w", err)
	}
	if body.Status == "" {
		return "", fmt.Errorf("external charge status: empty status")
	}
	return body.Status, nil
}
