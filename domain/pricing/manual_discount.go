package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// DiscountTypeManual identifies a manually-applied admin discount.
const DiscountTypeManual DiscountType = "manual"

type manualDiscountAuditProps struct {
	ManualDiscountID int      `json:"manual_discount_id"`
	OriginalType     string   `json:"original_type"`
	DiscountPct      *float64 `json:"discount_pct,omitempty"`
	FixedPrice       *float64 `json:"fixed_price,omitempty"`
	FixedCurrency    *string  `json:"fixed_currency,omitempty"`
}

// applyManualDiscount compares the active manual discount against the evaluation's current
// final price (after donation discount). It appends the manual discount to eval.Discounts
// for audit. If the manual discount yields a lower price (in NIS), it overrides FinalPrice
// and appends a line to Explain.
func applyManualDiscount(ctx context.Context, eval *V2PricingEvaluation, md *repo.ManualDiscount) {
	log := utils.LogFor(ctx)

	if md == nil {
		log.Info("applyManualDiscount: no active manual discount")
		eval.Discounts = append(eval.Discounts, Discount{
			Type:     DiscountTypeManual,
			Eligible: false,
		})
		return
	}
	log.Info("applyManualDiscount: found active discount",
		slog.Int("id", md.ID),
		slog.String("type", md.Type),
		slog.Bool("properties_valid", md.Properties.Valid),
		slog.Int("properties_len", len(md.Properties.JSON)),
	)

	var inputProps repo.ManualDiscountProperties
	if md.Properties.Valid && len(md.Properties.JSON) > 0 {
		if err := json.Unmarshal(md.Properties.JSON, &inputProps); err != nil {
			log.Warn("applyManualDiscount: failed to unmarshal properties",
				slog.Int("id", md.ID),
				slog.Any("err", err),
				slog.String("raw", string(md.Properties.JSON)),
			)
		}
	}

	base := eval.CountryBase
	currentFinalNIS := toNIS(eval.FinalPrice.Amount, eval.FinalPrice.Currency, USDToNIS, EURToNIS)

	var manualFinal Price
	var effectivePct float64
	var resolved bool

	switch md.Type {
	case "percent":
		if inputProps.DiscountPct != nil {
			pct := *inputProps.DiscountPct
			manualFinal = Price{
				Amount:   math.Round(base.Amount*(1-pct/100)*100) / 100,
				Currency: base.Currency,
			}
			effectivePct = pct
			resolved = true
		} else {
			log.Warn("applyManualDiscount: percent type but discount_pct is nil", slog.Int("id", md.ID))
		}

	case "fixed_price":
		if inputProps.FixedPrice != nil && inputProps.Currency != nil {
			manualFinal = Price{Amount: *inputProps.FixedPrice, Currency: *inputProps.Currency}
			baseNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS)
			if baseNIS > 0 {
				fixedNIS := toNIS(*inputProps.FixedPrice, *inputProps.Currency, USDToNIS, EURToNIS)
				effectivePct = math.Max(0, (1-fixedNIS/baseNIS)*100)
			}
			resolved = true
		} else {
			log.Warn("applyManualDiscount: fixed_price type but fixed_price/currency is nil", slog.Int("id", md.ID))
		}

	default:
		log.Warn("applyManualDiscount: unknown type", slog.Int("id", md.ID), slog.String("type", md.Type))
	}

	manualFinalNIS := toNIS(manualFinal.Amount, manualFinal.Currency, USDToNIS, EURToNIS)
	eligible := resolved && manualFinalNIS < currentFinalNIS
	log.Info("applyManualDiscount: result",
		slog.Int("id", md.ID),
		slog.Bool("resolved", resolved),
		slog.Bool("eligible", eligible),
		slog.Float64("manual_final_nis", manualFinalNIS),
		slog.Float64("current_final_nis", currentFinalNIS),
	)

	auditProps := manualDiscountAuditProps{
		ManualDiscountID: md.ID,
		OriginalType:     md.Type,
		DiscountPct:      inputProps.DiscountPct,
		FixedPrice:       inputProps.FixedPrice,
		FixedCurrency:    inputProps.Currency,
	}
	auditJSON, err := json.Marshal(auditProps)
	if err != nil {
		log.Warn("applyManualDiscount: failed to marshal audit props", slog.Int("id", md.ID), slog.Any("err", err))
	}

	eval.Discounts = append(eval.Discounts, Discount{
		Type:       DiscountTypeManual,
		AmountPct:  effectivePct,
		Eligible:   eligible,
		Properties: auditJSON,
	})

	if eligible {
		eval.FinalPrice = manualFinal
		eval.Explain = append(eval.Explain, fmt.Sprintf(
			"manual_discount[id=%d type=%s]: applied → %.2f %s",
			md.ID, md.Type, manualFinal.Amount, manualFinal.Currency,
		))
	}
}
