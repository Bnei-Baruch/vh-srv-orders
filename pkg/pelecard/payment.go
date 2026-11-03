package pelecard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
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

	// The card token and approval number renewals charge with. Empty until
	// external_payments stores them: today they travel only on the browser
	// redirect.
	Token      string `json:"Token"`
	ApprovalNo string `json:"ApprovalNo"`
}

// GetPayment asks external_payments what the gateway said about one payment,
// so this service does not have to take the facts from the browser.
func (c *Client) GetPayment(ctx context.Context, userKey string) (*GatewayPayment, error) {
	if c.Tokens == nil {
		return nil, fmt.Errorf("no token source configured for external_payments")
	}

	resp, err := c.sendAuthorized(ctx, "payment", func(r *resty.Request) (*resty.Response, error) {
		return r.Get(c.BaseURL + "/vh/payment/" + url.PathEscape(userKey))
	})
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode() == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: payment", ErrUnauthorized)
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
