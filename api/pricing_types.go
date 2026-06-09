package api

import (
	"encoding/json"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
)

// monthlyPriceResponse is the API DTO for the monthly price endpoint.
type monthlyPriceResponse struct {
	Amount         float64            `json:"amount"`
	Currency       string             `json:"currency"`
	PricingVersion string             `json:"pricing_version"`
	HasErrors      bool               `json:"has_errors,omitempty"`
	V2Details      *v2DetailsResponse `json:"v2_details,omitempty"`
	V1AllPrices    map[string]float64 `json:"v1_all_prices,omitempty"`
}

type v2DetailsResponse struct {
	EvaluatedAt time.Time                `json:"evaluated_at"`
	AccountID   int                      `json:"account_id"`
	CountryCode string                   `json:"country_code"`
	CountryBase pricing.CountryBasePrice `json:"country_base"`
	Discounts   []discountResponse       `json:"discounts"`
	FinalPrice  pricing.Price            `json:"final_price"`
	Explain     []string                 `json:"explain,omitempty"` // admin only
}

type discountResponse struct {
	Type       pricing.DiscountType `json:"type"`
	AmountPct  float64              `json:"amount_pct"`
	Eligible   bool                 `json:"eligible"`
	Error      bool                 `json:"error,omitempty"`
	Properties json.RawMessage      `json:"properties,omitempty"` // admin only
}

// toMonthlyPriceResponse maps a pricing result to the API DTO.
// Admin users receive the full v2 evaluation including Explain and Properties;
// non-admins receive the stripped version via eval.Public().
func toMonthlyPriceResponse(res *pricing.MonthlyPriceRes, isAdmin bool) monthlyPriceResponse {
	out := monthlyPriceResponse{
		Amount:         res.Amount.Float64,
		Currency:       res.Currency.String,
		PricingVersion: res.PricingVersion.String,
		V1AllPrices:    res.V1AllPrices,
	}
	if res.V2Details != nil {
		eval := res.V2Details
		out.HasErrors = eval.HasDiscountErrors()
		if !isAdmin {
			eval = eval.Public()
		}
		out.V2Details = toV2DetailsResponse(eval)
	}
	return out
}

func toV2DetailsResponse(eval *pricing.V2PricingEvaluation) *v2DetailsResponse {
	discounts := make([]discountResponse, len(eval.Discounts))
	for i, d := range eval.Discounts {
		discounts[i] = discountResponse{
			Type:       d.Type,
			AmountPct:  d.AmountPct,
			Eligible:   d.Eligible,
			Error:      d.Error,
			Properties: d.Properties,
		}
	}
	return &v2DetailsResponse{
		EvaluatedAt: eval.EvaluatedAt,
		AccountID:   eval.AccountID,
		CountryCode: eval.CountryCode,
		CountryBase: eval.CountryBase,
		Discounts:   discounts,
		FinalPrice:  eval.FinalPrice,
		Explain:     eval.Explain,
	}
}
