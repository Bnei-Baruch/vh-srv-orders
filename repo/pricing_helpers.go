package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/volatiletech/null/v9"
)

var europeanCountries = map[string]bool{
	"FRANCE":  true,
	"GERMANY": true,
	"SPAIN":   true,
	"ITALY":   true,
	"POLAND":  true,
	// Abbreviations
	"FR": true,
	"DE": true,
	"ES": true,
	"IT": true,
	"PL": true,
	// ... add all other European countries ...
}

func (o *OrdersDB) GetMonthlyPriceByKCID(ctx context.Context, kcID string) (*UserMonthlyPriceRes, error) {
	accountID, err := o.GetAccountIDByKeycloakID(ctx, kcID)
	if err != nil {
		return nil, fmt.Errorf("o.GetAccountIDByKeycloakID: %w", err)
	}

	account, err := o.GetAccount(ctx, accountID, "")
	if err != nil {
		return nil, fmt.Errorf("o.GetAccount: %w", err)
	}

	countryUpper := strings.ToUpper(account.Country.String)
	switch {
	case countryUpper == "ISRAEL" || countryUpper == "IL":
		return &UserMonthlyPriceRes{
			Amount:   null.Float64From(80.0),
			Currency: null.StringFrom("nis"),
		}, nil

	case europeanCountries[countryUpper]:
		return &UserMonthlyPriceRes{
			Amount:   null.Float64From(20.0),
			Currency: null.StringFrom("eur"),
		}, nil

	default:
		return &UserMonthlyPriceRes{
			Amount:   null.Float64From(20.0),
			Currency: null.StringFrom("usd"),
		}, nil
	}

}
