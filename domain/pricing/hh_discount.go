package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// DiscountTypeHH identifies a Help Haver grant discount.
const DiscountTypeHH DiscountType = "help_haver"

type hhDiscountAuditProps struct {
	HHGrantID   int       `json:"hh_grant_id"`
	GrantType   string    `json:"grant_type"`
	DiscountPct int       `json:"discount_pct"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// applyHHDiscount compares the active HH grant against the evaluation's current
// final price. It appends the HH discount to eval.Discounts for audit. If the grant
// yields a lower price (in NIS), it overrides FinalPrice and appends a line to Explain.
func applyHHDiscount(ctx context.Context, eval *V2PricingEvaluation, hh *repo.HHGrant) {
	log := utils.LogFor(ctx)

	if hh == nil {
		log.Info("applyHHDiscount: no active HH grant")
		eval.Discounts = append(eval.Discounts, Discount{
			Type:     DiscountTypeHH,
			Eligible: false,
		})
		return
	}
	log.Info("applyHHDiscount: found active grant",
		slog.Int("id", hh.ID),
		slog.String("type", hh.Type),
		slog.Int("discount_pct", hh.DiscountPct),
		slog.Time("end_date", hh.EndDate),
	)

	base := eval.CountryBase
	pct := float64(hh.DiscountPct)
	hhFinal := Price{
		Amount:   math.Round(base.Amount*(1-pct/100)*100) / 100,
		Currency: base.Currency,
	}

	currentFinalNIS := toNIS(eval.FinalPrice.Amount, eval.FinalPrice.Currency, USDToNIS, EURToNIS)
	hhFinalNIS := toNIS(hhFinal.Amount, hhFinal.Currency, USDToNIS, EURToNIS)
	eligible := hhFinalNIS < currentFinalNIS
	log.Info("applyHHDiscount: result",
		slog.Int("id", hh.ID),
		slog.Bool("eligible", eligible),
		slog.Float64("hh_final_nis", hhFinalNIS),
		slog.Float64("current_final_nis", currentFinalNIS),
	)

	auditJSON, err := json.Marshal(hhDiscountAuditProps{
		HHGrantID:   hh.ID,
		GrantType:   hh.Type,
		DiscountPct: hh.DiscountPct,
		ExpiresAt:   hh.EndDate,
	})
	if err != nil {
		log.Warn("applyHHDiscount: failed to marshal audit props", slog.Int("id", hh.ID), slog.Any("err", err))
	}

	eval.Discounts = append(eval.Discounts, Discount{
		Type:       DiscountTypeHH,
		AmountPct:  pct,
		Eligible:   eligible,
		Properties: auditJSON,
	})

	if eligible {
		eval.FinalPrice = hhFinal
		eval.Explain = append(eval.Explain, fmt.Sprintf(
			"help_haver[id=%d type=%s pct=%d expires=%s]: applied → %.2f %s",
			hh.ID, hh.Type, hh.DiscountPct, hh.EndDate.Format("2006-01-02"), hhFinal.Amount, hhFinal.Currency,
		))
	}
}
