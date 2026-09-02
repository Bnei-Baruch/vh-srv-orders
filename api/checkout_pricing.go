package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// sameAmount compares currency amounts at cent precision, so floating-point
// noise on either side never blocks a payment.
func sameAmount(a, b float64) bool {
	return math.Round(a*100) == math.Round(b*100)
}

// checkoutPriceEnforced reports whether the order must pay the server-resolved
// membership price. Offline and helphaver payments are recorded, not charged.
func checkoutPriceEnforced(req *repo.RequestOrder) bool {
	return req.ProductType.String == common.ProductTypeGlobalMembership &&
		req.PaymentType.String != common.PaymentTypeOffline &&
		req.PaymentType.String != common.PaymentTypeHelpHaver
}

// enforceCheckoutPrice resolves the account's v2 monthly price, rejects requests
// below it, and stamps the evaluation onto req so CreatePayment persists it.
// Returns false if it wrote an error response.
func (o *OrdersAPI) enforceCheckoutPrice(c *gin.Context, req *repo.RequestOrder, accountID int) bool {
	ctx := c.Request.Context()

	account, err := o.repo.GetAccount(ctx, accountID, "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("enforceCheckoutPrice: repo.GetAccount: %w", err))
		return false
	}

	res, err := pricing.GetMonthlyPrice(
		ctx,
		o.profileService, o.priorityClient, o.accountingService, o.quickbooksCompanyID,
		account.ID, account.UserKey.String, account.Email.String, account.Country.String,
		o.repo, o.repo, o.repo,
	)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("enforceCheckoutPrice: pricing.GetMonthlyPrice: %w", err))
		return false
	}
	if res.V2Details.HasDiscountErrors() {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("enforceCheckoutPrice: %w", pricing.ErrDonationFetch))
		return false
	}

	if !strings.EqualFold(req.Currency.String, res.Currency.String) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency does not match the current price",
			"amount": res.Amount.Float64, "currency": res.Currency.String})
		return false
	}
	if !sameAmount(req.Amount.Float64, res.Amount.Float64) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount does not match the current price",
			"amount": res.Amount.Float64, "currency": res.Currency.String})
		return false
	}
	// Charge and store the resolved price, rounded to cents in case discount math left float noise.
	req.Amount = null.Float64From(math.Round(res.Amount.Float64*100) / 100)

	evaluation, err := json.Marshal(res.V2Details)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("enforceCheckoutPrice: json.Marshal evaluation: %w", err))
		return false
	}
	req.PricingVersion = res.PricingVersion
	req.PricingEvaluation = null.JSONFrom(evaluation)
	return true
}
