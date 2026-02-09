package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/volatiletech/null/v9"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
)

func (o *OrdersDB) GetMonthlyPriceByKCID(ctx context.Context, kcID string, preferredCurrency string) (*UserMonthlyPriceRes, error) {
	accountID, err := o.GetAccountIDByKeycloakID(ctx, kcID)
	if err != nil {
		return nil, fmt.Errorf("o.GetAccountIDByKeycloakID: %w", err)
	}

	account, err := o.GetAccount(ctx, accountID, "")
	if err != nil {
		return nil, fmt.Errorf("o.GetAccount: %w", err)
	}

	// SCOPE: Dynamic pricing is enabled when one of the following conditions is met:
	// 1. User is from Israel (Country = IL)
	// 2. User's preferred currency is NIS (from frontend localStorage)
	// If neither condition is met, return nil to signal static pricing should be used

	preferredCurrency = strings.ToLower(preferredCurrency)

	if account.Country.String != "IL" && preferredCurrency != "nis" {
		return nil, nil
	}

	basePrice := pricing.GetCountryBasePrice(account.Country.String)
	return &UserMonthlyPriceRes{
		Amount:   null.Float64From(basePrice.Amount),
		Currency: null.StringFrom(basePrice.Currency),
	}, nil
}
