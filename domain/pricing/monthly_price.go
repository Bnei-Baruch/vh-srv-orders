package pricing

import (
	"context"
	"strings"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// MonthlyPriceRes is the response for a monthly price query.
type MonthlyPriceRes struct {
	Amount         null.Float64         `json:"amount"`
	Currency       null.String          `json:"currency"`
	PricingVersion null.String          `json:"pricing_version"`
	V2Details      *V2PricingEvaluation `json:"v2_details,omitempty"`
	V1AllPrices    map[string]float64   `json:"v1_all_prices,omitempty"`
}

// v1Pricing contains static monthly prices for legacy v1 pricing.
var v1Pricing = map[string]Price{
	common.CurrencyUSD: {Amount: 20, Currency: common.CurrencyUSD},
	common.CurrencyEUR: {Amount: 20, Currency: common.CurrencyEUR},
	common.CurrencyNIS: {Amount: 80, Currency: common.CurrencyNIS},
}

// GetMonthlyPrice resolves the monthly price for a member account.
// Pass pricingVersion "v1" or "v2" to force a specific version.
// Any other value (including empty) auto-routes via V2Eligible — the same logic billing uses.
// discountProvider is used to fetch and apply a manual discount after v2 evaluation.
// Pass nil to skip manual discount lookup.
func GetMonthlyPrice(
	ctx context.Context,
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	accountingService accounting.AccountingService,
	quickbooksCompanyID string,
	accountID int,
	keycloakID string,
	email string,
	country string,
	preferredCurrency string,
	pricingVersion string,
	discountProvider repo.ManualDiscountProvider,
	hhProvider repo.HHGrantProvider,
) (*MonthlyPriceRes, error) {
	if preferredCurrency == "" {
		preferredCurrency = common.CurrencyUSD
	}

	var (
		price Price
		res   MonthlyPriceRes
	)

	switch pricingVersion {
	case "v1":
		// v1 pricing is static; manual discounts are not applied by design.
		price = selectV1Price(preferredCurrency)
		res.V1AllPrices = allV1Prices()

	case "v2":
		v2eval, err := EvaluateV2Price(ctx, profileService, priorityClient, accountingService, quickbooksCompanyID, accountID, keycloakID, email, country, discountProvider, hhProvider)
		if err != nil {
			return nil, err
		}
		price = v2eval.FinalPrice
		res.V2Details = v2eval

	default:
		// Auto-route using the same eligibility criteria as billing.
		if V2Eligible(country) {
			pricingVersion = "v2"
			v2eval, err := EvaluateV2Price(ctx, profileService, priorityClient, accountingService, quickbooksCompanyID, accountID, keycloakID, email, country, discountProvider, hhProvider)
			if err != nil {
				return nil, err
			}
			price = v2eval.FinalPrice
			res.V2Details = v2eval
		} else {
			// v1 pricing is static; manual discounts are not applied by design.
			pricingVersion = "v1"
			price = selectV1Price(preferredCurrency)
			res.V1AllPrices = allV1Prices()
		}
	}

	res.Amount = null.Float64From(price.Amount)
	res.Currency = null.StringFrom(price.Currency)
	res.PricingVersion = null.StringFrom(pricingVersion)
	return &res, nil
}

func selectV1Price(currency string) Price {
	if p, ok := v1Pricing[currency]; ok {
		return p
	}
	return v1Pricing[common.CurrencyUSD]
}

func allV1Prices() map[string]float64 {
	result := make(map[string]float64, len(v1Pricing))
	for currency, price := range v1Pricing {
		result[strings.ToLower(currency)] = price.Amount
	}
	return result
}
