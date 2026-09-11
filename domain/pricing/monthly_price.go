package pricing

import (
	"context"

	"github.com/volatiletech/null/v9"

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
}

// GetMonthlyPrice resolves the monthly price for a member account via v2 evaluation.
// discountProvider/hhProvider/couponProvider apply their respective discounts; pass nil to skip.
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
	discountProvider repo.ManualDiscountProvider,
	hhProvider repo.HHGrantProvider,
	couponProvider repo.CouponProvider,
) (*MonthlyPriceRes, error) {
	v2eval, err := EvaluateV2Price(ctx, profileService, priorityClient, accountingService, quickbooksCompanyID, accountID, keycloakID, email, country, discountProvider, hhProvider, couponProvider)
	if err != nil {
		return nil, err
	}
	return &MonthlyPriceRes{
		Amount:         null.Float64From(v2eval.FinalPrice.Amount),
		Currency:       null.StringFrom(v2eval.FinalPrice.Currency),
		PricingVersion: null.StringFrom("v2"),
		V2Details:      v2eval,
	}, nil
}
