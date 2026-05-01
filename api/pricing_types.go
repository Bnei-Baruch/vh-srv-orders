package api

import (
	"encoding/json"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// monthlyPriceResponse is the API DTO for the monthly price endpoint.
// It owns the "degraded" concept (donation fetch error) — the pricing package
// always returns errors; this layer decides what to present to the client.
type monthlyPriceResponse struct {
	Amount         float64            `json:"amount"`
	Currency       string             `json:"currency"`
	PricingVersion string             `json:"pricing_version"`
	V1Details      *v1DetailsResponse `json:"v1_details,omitempty"`
	V1AllPrices    map[string]float64 `json:"v1_all_prices,omitempty"`
	V2Details      *v2DetailsResponse `json:"v2_details,omitempty"`
}

type v1DetailsResponse struct {
	EvaluatedAt time.Time          `json:"evaluated_at"`
	AccountID   int                `json:"account_id"`
	BasePrice   pricing.Price      `json:"base_price"`
	Discounts   []discountResponse `json:"discounts"`
	FinalPrice  pricing.Price      `json:"final_price"`
	Explain     []string           `json:"explain,omitempty"` // admin only
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
// Admin users receive the full evaluation including Explain and Properties;
// non-admins receive the stripped version via eval.Public().
func toMonthlyPriceResponse(res *pricing.MonthlyPriceRes, isAdmin bool) monthlyPriceResponse {
	out := monthlyPriceResponse{
		Amount:         res.Amount.Float64,
		Currency:       res.Currency.String,
		PricingVersion: res.PricingVersion.String,
		V1AllPrices:    res.V1AllPrices,
	}
	if res.V1Details != nil {
		eval := res.V1Details
		if !isAdmin {
			eval = eval.Public()
		}
		out.V1Details = toV1DetailsResponse(eval)
	}
	if res.V2Details != nil {
		eval := res.V2Details
		if !isAdmin {
			eval = eval.Public()
		}
		out.V2Details = toV2DetailsResponse(eval)
	}
	return out
}

func toV1DetailsResponse(eval *pricing.V1PricingEvaluation) *v1DetailsResponse {
	discounts := make([]discountResponse, len(eval.Discounts))
	for i, d := range eval.Discounts {
		discounts[i] = discountResponse{
			Type:       d.Type,
			AmountPct:  d.AmountPct,
			Eligible:   d.Eligible,
			Properties: d.Properties,
		}
	}
	return &v1DetailsResponse{
		EvaluatedAt: eval.EvaluatedAt,
		AccountID:   eval.AccountID,
		BasePrice:   eval.BasePrice,
		Discounts:   discounts,
		FinalPrice:  eval.FinalPrice,
		Explain:     eval.Explain,
	}
}

func toV2DetailsResponse(eval *pricing.V2PricingEvaluation) *v2DetailsResponse {
	discounts := make([]discountResponse, len(eval.Discounts))
	for i, d := range eval.Discounts {
		discounts[i] = discountResponse{
			Type:       d.Type,
			AmountPct:  d.AmountPct,
			Eligible:   d.Eligible,
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

// buildDegradedMonthlyPriceResponse builds a response for when the Priority donation
// API is unreachable. Returns base price with Error=true on the donations discount so
// the client can show an appropriate message without blocking the user.
func buildDegradedMonthlyPriceResponse(account *repo.Account) monthlyPriceResponse {
	base := pricing.GetCountryBasePrice(account.Country.String)
	return monthlyPriceResponse{
		Amount:         base.Amount,
		Currency:       base.Currency,
		PricingVersion: "v2",
		V2Details: &v2DetailsResponse{
			EvaluatedAt: time.Now().UTC(),
			AccountID:   account.ID,
			CountryCode: account.Country.String,
			CountryBase: base,
			FinalPrice:  pricing.Price{Amount: base.Amount, Currency: base.Currency},
			Discounts: []discountResponse{{
				Type:      pricing.DiscountTypeDonations,
				AmountPct: pricing.DonationsDiscountAmtPct,
				Eligible:  false,
				Error:     true,
			}},
		},
	}
}
