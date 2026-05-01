package pricing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// V1PricingEvaluation is the record of a v1 pricing evaluation.
// It captures the base price, any HH discount, and the final price for audit and debugging.
type V1PricingEvaluation struct {
	EvaluatedAt time.Time  `json:"evaluated_at"`
	AccountID   int        `json:"account_id"`
	BasePrice   Price      `json:"base_price"`
	Discounts   []Discount `json:"discounts"`
	FinalPrice  Price      `json:"final_price"`
	Explain     []string   `json:"explain"`
}

// Public returns a copy with admin-only fields stripped (Explain and Discount.Properties).
func (e *V1PricingEvaluation) Public() *V1PricingEvaluation {
	pub := *e
	pub.Explain = nil
	pub.Discounts = make([]Discount, len(e.Discounts))
	for i, d := range e.Discounts {
		pub.Discounts[i] = Discount{
			Type:      d.Type,
			AmountPct: d.AmountPct,
			Eligible:  d.Eligible,
			Applied:   d.Applied,
		}
	}
	return &pub
}

// v1Pricing contains the static monthly base prices for v1 pricing.
var v1Pricing = map[string]Price{
	common.CurrencyUSD: {Amount: 20, Currency: common.CurrencyUSD},
	common.CurrencyEUR: {Amount: 20, Currency: common.CurrencyEUR},
	common.CurrencyNIS: {Amount: 80, Currency: common.CurrencyNIS},
}

// selectV1BasePrice returns the static v1 base price for the given currency.
// Falls back to USD for unknown currencies.
func selectV1BasePrice(currency string) Price {
	if p, ok := v1Pricing[currency]; ok {
		return p
	}
	return v1Pricing[common.CurrencyUSD]
}

// evaluateV1 resolves the v1 price for a member, applying an HH grant discount if active.
// Only HH discounts apply in v1 — no donation-based discounts.
func evaluateV1(ctx context.Context, profileService profiles.ProfileService, accountID int, keycloakID string, currency string) (*V1PricingEvaluation, error) {
	log := utils.LogFor(ctx)
	base := selectV1BasePrice(currency)
	log.Info("evaluateV1: start",
		slog.Int("account_id", accountID),
		slog.String("keycloak_id", keycloakID),
		slog.String("currency", currency),
		slog.Float64("base_amount", base.Amount),
		slog.String("base_currency", base.Currency),
	)

	if keycloakID == "" {
		return nil, fmt.Errorf("evaluateV1: keycloakID is required")
	}

	eval := &V1PricingEvaluation{
		EvaluatedAt: time.Now().UTC(),
		AccountID:   accountID,
		BasePrice:   base,
		Discounts:   []Discount{},
	}

	hhGrant, err := profileService.GetActiveHHGrant(ctx, keycloakID)
	if err != nil {
		log.Error("evaluateV1: GetActiveHHGrant failed — skipping HH discount",
			slog.Any("err", err),
			slog.String("keycloak_id", keycloakID),
		)
	} else if hhGrant == nil {
		log.Info("evaluateV1: no active HH grant", slog.String("keycloak_id", keycloakID))
	} else if hhGrant.IsExpired() {
		log.Info("evaluateV1: HH grant expired",
			slog.String("keycloak_id", keycloakID),
			slog.Time("expires_at", hhGrant.ExpiresAt),
		)
	} else {
		log.Info("evaluateV1: HH grant active — applying discount",
			slog.String("keycloak_id", keycloakID),
			slog.Time("expires_at", hhGrant.ExpiresAt),
			slog.Any("discount_pct", hhGrant.DiscountPct),
		)
		eval.Discounts = append(eval.Discounts, buildHHDiscount(hhGrant))
	}

	eval.FinalPrice = resolveFinalPrice(eval.Discounts, base, USDToNIS, EURToNIS)

	explain := make([]string, 0, 3)
	explain = append(explain, fmt.Sprintf("base: %.2f %s/mo", base.Amount, base.Currency))
	for _, d := range eval.Discounts {
		if d.Type == DiscountTypeHH {
			explain = append(explain, hhExplainLine(d))
		}
	}
	explain = append(explain, fmt.Sprintf("final: primary[#%d] %.2f %s", accountID, eval.FinalPrice.Amount, eval.FinalPrice.Currency))
	eval.Explain = explain

	log.Info("evaluateV1: done",
		slog.Float64("final_amount", eval.FinalPrice.Amount),
		slog.String("final_currency", eval.FinalPrice.Currency),
	)
	return eval, nil
}
