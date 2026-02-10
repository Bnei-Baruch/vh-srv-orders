package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/volatiletech/null/v9"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
)

// V1 pricing: Static pricing used in frontend (legacy)
var v1Pricing = map[string]pricing.CountryBasePrice{
	"usd": {Currency: "usd", Amount: 20},
	"eur": {Currency: "eur", Amount: 20},
	"nis": {Currency: "nis", Amount: 80},
}

func (o *OrdersDB) GetMonthlyPriceByKCID(ctx context.Context, kcID string, preferredCurrency string, pricingVersion string) (*UserMonthlyPriceRes, error) {
	accountID, err := o.GetAccountIDByKeycloakID(ctx, kcID)
	if err != nil {
		return nil, fmt.Errorf("o.GetAccountIDByKeycloakID: %w", err)
	}

	account, err := o.GetAccount(ctx, accountID, "")
	if err != nil {
		return nil, fmt.Errorf("o.GetAccount: %w", err)
	}

	preferredCurrency = strings.ToLower(preferredCurrency)
	if preferredCurrency == "" {
		preferredCurrency = "usd" // Default currency
	}

	// Determine which pricing version to use
	// Default: v1 (backward compatible)
	if pricingVersion == "" {
		pricingVersion = "v1"
	}

	var price pricing.CountryBasePrice

	switch pricingVersion {
	case "v1":
		// V1: Static pricing (legacy frontend pricing)
		if p, ok := v1Pricing[preferredCurrency]; ok {
			price = p
		} else {
			price = v1Pricing["usd"] // Fallback to USD
		}

	case "v2":
		// V2: Country-based tiered pricing
		price = pricing.GetCountryBasePrice(account.Country.String)

	case "t1":
		// T1 (Tier 1): Special case for Israel or NIS currency
		// Scope: Country=IL OR preferredCurrency=nis
		// If in scope, use v2 pricing; otherwise use v1 pricing
		// NOTE: Additional tiers (t2, t3, etc.) will be implemented in future iterations
		// to gradually roll out country-based pricing to other regions
		if account.Country.String == "IL" || preferredCurrency == "nis" {
			price = pricing.GetCountryBasePrice(account.Country.String)
		} else {
			if p, ok := v1Pricing[preferredCurrency]; ok {
				price = p
			} else {
				price = v1Pricing["usd"]
			}
		}

	default:
		// Unknown version, fallback to v1
		if p, ok := v1Pricing[preferredCurrency]; ok {
			price = p
		} else {
			price = v1Pricing["usd"]
		}
	}

	return &UserMonthlyPriceRes{
		Amount:         null.Float64From(price.Amount),
		Currency:       null.StringFrom(price.Currency),
		PricingVersion: null.StringFrom(pricingVersion),
	}, nil
}
