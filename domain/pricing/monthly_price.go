package pricing

import (
	"context"
	"fmt"
	"strings"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

// MonthlyPriceRes is the response for a monthly price query.
type MonthlyPriceRes struct {
	Amount         null.Float64         `json:"amount"`
	Currency       null.String          `json:"currency"`
	PricingVersion null.String          `json:"pricing_version"`
	V1Details      *V1PricingEvaluation `json:"v1_details,omitempty"`
	V1AllPrices    map[string]float64   `json:"v1_all_prices,omitempty"`
	V2Details      *V2PricingEvaluation `json:"v2_details,omitempty"`
}

// GetMonthlyPrice resolves the monthly price for a member account.
// Pass pricingVersion "v1" or "v2" to force a specific version.
// Any other value (including empty) auto-routes via V2Eligible — the same logic billing uses.
func GetMonthlyPrice(
	ctx context.Context,
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	accountID int,
	keycloakID string,
	email string,
	country string,
	preferredCurrency string,
	pricingVersion string,
) (*MonthlyPriceRes, error) {
	if preferredCurrency == "" {
		preferredCurrency = common.CurrencyUSD
	}

	// Resolve version: explicit "v1"/"v2" wins; otherwise auto-route by country eligibility.
	if pricingVersion != "v1" && pricingVersion != "v2" {
		if V2Eligible(country) {
			pricingVersion = "v2"
		} else {
			pricingVersion = "v1"
		}
	}

	var (
		price Price
		res   MonthlyPriceRes
	)

	switch pricingVersion {
	case "v1":
		v1eval, err := evaluateV1(ctx, profileService, accountID, keycloakID, preferredCurrency)
		if err != nil {
			return nil, err
		}
		price = v1eval.FinalPrice
		res.V1Details = v1eval
		res.V1AllPrices = allV1Prices()
	case "v2":
		v2eval, err := evaluateV2(ctx, profileService, priorityClient, accountID, keycloakID, email, country)
		if err != nil {
			return nil, err
		}
		price = v2eval.FinalPrice
		res.V2Details = v2eval
	}

	res.Amount = null.Float64From(price.Amount)
	res.Currency = null.StringFrom(price.Currency)
	res.PricingVersion = null.StringFrom(pricingVersion)
	return &res, nil
}

func allV1Prices() map[string]float64 {
	result := make(map[string]float64, len(v1Pricing))
	for currency, price := range v1Pricing {
		result[strings.ToLower(currency)] = price.Amount
	}
	return result
}

func evaluateV2(
	ctx context.Context,
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	accountID int,
	keycloakID string,
	email string,
	country string,
) (*V2PricingEvaluation, error) {
	if common.Config.PriorityBaseURL == "" {
		return nil, fmt.Errorf("v2 pricing requires PRIORITY_BASE_URL to be configured")
	}
	return EvaluateV2Price(ctx, profileService, priorityClient, accountID, keycloakID, email, country)
}
