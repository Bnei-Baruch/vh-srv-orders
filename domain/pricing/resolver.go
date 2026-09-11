package pricing

import (
	"context"
	"fmt"

	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// ChargePrice is the resolved price for a billing charge.
type ChargePrice struct {
	Amount         float64              `json:"amount"`
	Currency       string               `json:"currency"`
	PricingVersion string               `json:"pricing_version"`
	V2Evaluation   *V2PricingEvaluation `json:"v2_evaluation,omitempty"`
}

// PriceResolver resolves v2 charge prices for billing based on the account's country.
type PriceResolver struct {
	profileService      profiles.ProfileService
	priorityClient      *priority.Client
	accountingService   accounting.AccountingService
	quickbooksCompanyID string
	discountProvider    repo.ManualDiscountProvider // optional; applies manual discount when set
	hhProvider          repo.HHGrantProvider        // optional; applies Help Haver grant when set
	couponProvider      repo.CouponProvider         // optional; applies redeemed coupons when set
}

// NewPriceResolver creates a resolver for billing use.
func NewPriceResolver(
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	accountingService accounting.AccountingService,
	quickbooksCompanyID string,
) *PriceResolver {
	return &PriceResolver{
		profileService:      profileService,
		priorityClient:      priorityClient,
		accountingService:   accountingService,
		quickbooksCompanyID: quickbooksCompanyID,
	}
}

// SetManualDiscountProvider wires the manual discount lookup into the resolver.
// Call this after NewPriceResolver when a DB is available (e.g. billing commands).
func (r *PriceResolver) SetManualDiscountProvider(p repo.ManualDiscountProvider) {
	r.discountProvider = p
}

// SetHHGrantProvider wires the Help Haver grant lookup into the resolver.
// Call this after NewPriceResolver when a DB is available (e.g. billing commands).
func (r *PriceResolver) SetHHGrantProvider(p repo.HHGrantProvider) {
	r.hhProvider = p
}

// SetCouponProvider wires the coupon-redemption lookup into the resolver.
// Call this after NewPriceResolver when a DB is available (e.g. billing commands).
func (r *PriceResolver) SetCouponProvider(p repo.CouponProvider) {
	r.couponProvider = p
}

// Resolve determines the charge price for a renewal by evaluating country-based
// pricing with donation discounts.
func (r *PriceResolver) Resolve(ctx context.Context, account *repo.Account) (*ChargePrice, error) {
	eval, err := EvaluateV2Price(
		ctx, r.profileService, r.priorityClient, r.accountingService, r.quickbooksCompanyID,
		account.ID, account.UserKey.String, account.Email.String, account.Country.String,
		r.discountProvider, r.hhProvider, r.couponProvider,
	)
	if err != nil {
		return nil, fmt.Errorf("EvaluateV2Price: %w", err)
	}
	if eval.HasDiscountErrors() {
		return nil, fmt.Errorf("EvaluateV2Price: %w", ErrDonationFetch)
	}
	return &ChargePrice{
		Amount:         eval.FinalPrice.Amount,
		Currency:       eval.FinalPrice.Currency,
		PricingVersion: "v2",
		V2Evaluation:   eval,
	}, nil
}
