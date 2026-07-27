package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/domain/coupon"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// DiscountTypeCoupon identifies a redeemed-coupon discount.
const DiscountTypeCoupon DiscountType = "coupon"

type couponAuditProps struct {
	CouponID     int       `json:"coupon_id"`
	Code         string    `json:"code"`
	RedemptionID int       `json:"redemption_id"`
	BenefitEnd   time.Time `json:"benefit_end"`
	SkipReason   string    `json:"skip_reason,omitempty"`
}

// applyCouponDiscounts evaluates every active redemption as an independent
// best-price candidate (no stacking). fixed_price candidates are skipped when
// their currency no longer matches the member's pricing currency (decision #8).
// The single lowest candidate below the current price wins and updates FinalPrice;
// all candidates are appended to eval.Discounts for audit. When the member has no
// active redemption, a single ineligible coupon row is emitted for consistency
// with the always-present donations/HH/manual rows.
func applyCouponDiscounts(ctx context.Context, eval *V2PricingEvaluation, redemptions []repo.ActiveCouponRedemption) {
	log := utils.LogFor(ctx)
	if len(redemptions) == 0 {
		eval.Discounts = append(eval.Discounts, Discount{Type: DiscountTypeCoupon, Eligible: false})
		return
	}
	base := eval.CountryBase
	currentNIS := toNIS(eval.FinalPrice.Amount, eval.FinalPrice.Currency, USDToNIS, EURToNIS)

	type candidate struct {
		price Price
		pct   float64
		ok    bool
		skip  string
	}
	cands := make([]candidate, len(redemptions))
	bestIdx := -1
	bestNIS := currentNIS

	for i, r := range redemptions {
		price, pct, ok, skip := couponCandidatePrice(base, r)
		cands[i] = candidate{price, pct, ok, skip}
		if !ok {
			continue
		}
		if nis := toNIS(price.Amount, price.Currency, USDToNIS, EURToNIS); nis < bestNIS {
			bestNIS = nis
			bestIdx = i
		}
	}

	for i, r := range redemptions {
		eligible := i == bestIdx
		auditJSON, err := json.Marshal(couponAuditProps{
			CouponID: r.CouponID, Code: r.Code, RedemptionID: r.RedemptionID,
			BenefitEnd: r.BenefitEnd, SkipReason: cands[i].skip,
		})
		if err != nil {
			log.Warn("applyCouponDiscounts: failed to marshal audit props",
				slog.Int("redemption_id", r.RedemptionID), slog.Any("err", err))
		}
		eval.Discounts = append(eval.Discounts, Discount{
			Type:       DiscountTypeCoupon,
			AmountPct:  cands[i].pct,
			Eligible:   eligible,
			Properties: auditJSON,
		})
	}

	if bestIdx >= 0 {
		win := redemptions[bestIdx]
		eval.FinalPrice = cands[bestIdx].price
		eval.Explain = append(eval.Explain, fmt.Sprintf(
			"coupon[id=%d code=%s redemption=%d]: applied → %.2f %s",
			win.CouponID, win.Code, win.RedemptionID, eval.FinalPrice.Amount, eval.FinalPrice.Currency))
	}
}

// couponCandidatePrice resolves the price a redemption would yield. ok is false
// (with a skip reason) when a fixed_price coupon's currency does not match the
// member's current pricing currency.
func couponCandidatePrice(base CountryBasePrice, r repo.ActiveCouponRedemption) (Price, float64, bool, string) {
	var props repo.CouponProperties
	if r.Properties.Valid && len(r.Properties.JSON) > 0 {
		if err := json.Unmarshal(r.Properties.JSON, &props); err != nil {
			return Price{}, 0, false, "invalid coupon properties"
		}
	}

	switch r.Type {
	case coupon.TypePercent:
		if props.DiscountPct == nil {
			return Price{}, 0, false, "missing discount_pct"
		}
		pct := *props.DiscountPct
		return Price{
			Amount:   math.Round(base.Amount*(1-pct/100)*100) / 100,
			Currency: base.Currency,
		}, pct, true, ""

	case coupon.TypeFixedPrice:
		if props.FixedPrice == nil || props.Currency == nil {
			return Price{}, 0, false, "missing fixed_price/currency"
		}
		// Coupons are not country-scoped; a fixed price keeps its own currency and
		// is ranked in NIS, exactly like a fixed_price manual discount.
		fixed := Price{Amount: *props.FixedPrice, Currency: *props.Currency}
		var pct float64
		baseNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS)
		if baseNIS > 0 {
			fixedNIS := toNIS(fixed.Amount, fixed.Currency, USDToNIS, EURToNIS)
			pct = math.Round(math.Max(0, (1-fixedNIS/baseNIS)*100)*100) / 100
		}
		return fixed, pct, true, ""

	default:
		return Price{}, 0, false, "unknown coupon type"
	}
}
