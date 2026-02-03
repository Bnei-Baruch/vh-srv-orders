package repo

import (
	"context"
	"fmt"

	"github.com/volatiletech/null/v9"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
)

func (o *OrdersDB) GetMonthlyPriceByKCID(ctx context.Context, kcID string) (*UserMonthlyPriceRes, error) {
	accountID, err := o.GetAccountIDByKeycloakID(ctx, kcID)
	if err != nil {
		return nil, fmt.Errorf("o.GetAccountIDByKeycloakID: %w", err)
	}

	account, err := o.GetAccount(ctx, accountID, "")
	if err != nil {
		return nil, fmt.Errorf("o.GetAccount: %w", err)
	}

	basePrice := pricing.GetCountryBasePrice(account.Country.String)
	return &UserMonthlyPriceRes{
		Amount:   null.Float64From(basePrice.Amount),
		Currency: null.StringFrom(basePrice.Currency),
	}, nil
}
