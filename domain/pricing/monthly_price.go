package pricing

import (
	"context"
	"fmt"

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
	V2Details      *V2PricingEvaluation `json:"v2_details,omitempty"`
}

// v1Pricing contains static monthly prices for legacy v1 pricing.
var v1Pricing = map[string]Price{
	common.CurrencyUSD: {Amount: 20, Currency: common.CurrencyUSD},
	common.CurrencyEUR: {Amount: 20, Currency: common.CurrencyEUR},
	common.CurrencyNIS: {Amount: 80, Currency: common.CurrencyNIS},
}

// GetMonthlyPrice resolves the monthly price for a member account.
// Supported versions: v1 (static), v2 (country-based), t1 (tier-1 rollout: IL/NIS → v2, others → v1).
func GetMonthlyPrice(
	ctx context.Context,
	accounts AccountRepository,
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
	if pricingVersion == "" {
		pricingVersion = "v2"
	}

	var (
		price Price
		res   MonthlyPriceRes
	)

	switch pricingVersion {
	case "v1":
		price = selectV1Price(preferredCurrency)

	case "v2":
		v2eval, err := evaluateV2(ctx, accounts, profileService, priorityClient, accountID, keycloakID, email, country)
		if err != nil {
			return nil, err
		}
		price = v2eval.FinalPrice
		res.V2Details = v2eval

	case "t1":
		if country == "IL" || preferredCurrency == common.CurrencyNIS {
			v2eval, err := evaluateV2(ctx, accounts, profileService, priorityClient, accountID, keycloakID, email, country)
			if err != nil {
				return nil, err
			}
			price = v2eval.FinalPrice
			res.V2Details = v2eval
		} else {
			price = selectV1Price(preferredCurrency)
		}

	default:
		price = selectV1Price(preferredCurrency)
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

func evaluateV2(
	ctx context.Context,
	accounts AccountRepository,
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
	return EvaluateV2Price(ctx, accounts, profileService, priorityClient, accountID, keycloakID, email, country)
}
