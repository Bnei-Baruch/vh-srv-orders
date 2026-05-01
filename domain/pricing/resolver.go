package pricing

import (
	"context"
	"fmt"

	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ChargePrice is the resolved price for a billing charge.
type ChargePrice struct {
	Amount         float64              `json:"amount"`
	Currency       string               `json:"currency"`
	PricingVersion string               `json:"pricing_version"`
	V1Evaluation   *V1PricingEvaluation `json:"v1_evaluation,omitempty"`
	V2Evaluation   *V2PricingEvaluation `json:"v2_evaluation,omitempty"`
}

// PriceResolver resolves charge prices for billing. It routes between v1 and v2 based on
// the account's country.
type PriceResolver struct {
	profileService profiles.ProfileService
	priorityClient *priority.Client
}

// NewPriceResolver creates a resolver for billing use.
func NewPriceResolver(
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
) *PriceResolver {
	return &PriceResolver{
		profileService: profileService,
		priorityClient: priorityClient,
	}
}

// Resolve determines the charge price for a renewal.
// For v2-eligible countries, EvaluateV2Price handles all discounts (donations + HH).
// For other countries, static v1 pricing is used with an HH grant applied if present.
func (r *PriceResolver) Resolve(ctx context.Context, account *repo.Account, v1OrderCurrency string) (*ChargePrice, error) {
	if V2Eligible(account.Country.String) {
		eval, err := evaluateV2(
			ctx, r.profileService, r.priorityClient,
			account.ID, account.UserKey.String, account.Email.String, account.Country.String,
		)
		if err != nil {
			return nil, fmt.Errorf("evaluateV2: %w", err)
		}
		return &ChargePrice{
			Amount:         eval.FinalPrice.Amount,
			Currency:       eval.FinalPrice.Currency,
			PricingVersion: "v2",
			V2Evaluation:   eval,
		}, nil
	}

	eval, err := evaluateV1(ctx, r.profileService, account.ID, account.UserKey.String, v1OrderCurrency)
	if err != nil {
		return nil, fmt.Errorf("evaluateV1: %w", err)
	}
	return &ChargePrice{
		Amount:         eval.FinalPrice.Amount,
		Currency:       eval.FinalPrice.Currency,
		PricingVersion: "v1",
		V1Evaluation:   eval,
	}, nil
}
