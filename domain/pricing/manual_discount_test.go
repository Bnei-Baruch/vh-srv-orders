package pricing

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// baseEval returns a minimal V2PricingEvaluation for IL (NIS base = 80.0).
func baseEval() *V2PricingEvaluation {
	return &V2PricingEvaluation{
		CountryCode: "IL",
		CountryBase: CountryBasePrice{Price: Price{Amount: 80.0, Currency: "NIS"}, Group: "IL"},
		FinalPrice:  Price{Amount: 80.0, Currency: "NIS"},
	}
}

func pctDiscount(id int, pct float64) *repo.ManualDiscount {
	props, _ := json.Marshal(repo.ManualDiscountProperties{DiscountPct: &pct})
	return &repo.ManualDiscount{ID: id, Type: "percent", Properties: null.JSONFrom(props)}
}

func fixedDiscount(id int, price float64, currency string) *repo.ManualDiscount {
	props, _ := json.Marshal(repo.ManualDiscountProperties{FixedPrice: &price, Currency: &currency})
	return &repo.ManualDiscount{ID: id, Type: "fixed_price", Properties: null.JSONFrom(props)}
}

func TestApplyManualDiscount_NilDiscount_AddsIneligibleEntry(t *testing.T) {
	eval := baseEval()
	applyManualDiscount(context.Background(), eval, nil)
	require.Len(t, eval.Discounts, 1)
	d := eval.Discounts[0]
	assert.Equal(t, DiscountTypeManual, d.Type)
	assert.False(t, d.Eligible)
	assert.False(t, d.Error)
	assert.Equal(t, 80.0, eval.FinalPrice.Amount, "final price unchanged")
	assert.Empty(t, eval.Explain, "explain unchanged when no discount")
}

func TestApplyManualDiscount_PercentEligible(t *testing.T) {
	eval := baseEval() // base = 80 NIS, final = 80 NIS
	applyManualDiscount(context.Background(), eval, pctDiscount(1, 50))

	require.Len(t, eval.Discounts, 1)
	d := eval.Discounts[0]
	assert.Equal(t, DiscountTypeManual, d.Type)
	assert.Equal(t, 50.0, d.AmountPct)
	assert.True(t, d.Eligible)
	// 80 * (1 - 50/100) = 40 NIS
	assert.Equal(t, 40.0, eval.FinalPrice.Amount)
	assert.Equal(t, "NIS", eval.FinalPrice.Currency)
}

func TestApplyManualDiscount_PercentNotEligible_PriceWouldBeHigher(t *testing.T) {
	// base = 80 NIS, but final has already been reduced to 40 NIS (e.g. donations discount)
	eval := baseEval()
	eval.FinalPrice = Price{Amount: 40.0, Currency: "NIS"}
	// manual 10% off base = 72 NIS which is > 40 NIS (current final)
	applyManualDiscount(context.Background(), eval, pctDiscount(2, 10))

	require.Len(t, eval.Discounts, 1)
	assert.False(t, eval.Discounts[0].Eligible, "manual discount should not be applied when it yields a higher price")
	assert.Equal(t, 40.0, eval.FinalPrice.Amount, "final price should be unchanged")
}

func TestApplyManualDiscount_FixedPriceEligible(t *testing.T) {
	eval := baseEval() // base = 80 NIS, final = 80 NIS
	applyManualDiscount(context.Background(), eval, fixedDiscount(3, 50.0, "NIS"))

	require.Len(t, eval.Discounts, 1)
	d := eval.Discounts[0]
	assert.True(t, d.Eligible)
	assert.Equal(t, 50.0, eval.FinalPrice.Amount)
	assert.Equal(t, "NIS", eval.FinalPrice.Currency)
}

func TestApplyManualDiscount_FixedPriceInDifferentCurrency(t *testing.T) {
	eval := baseEval() // base = 80 NIS, final = 80 NIS; 80 NIS = ~25.8 USD at 3.1
	applyManualDiscount(context.Background(), eval, fixedDiscount(4, 10.0, "USD")) // 10 USD < 25.8 USD equiv

	require.Len(t, eval.Discounts, 1)
	assert.True(t, eval.Discounts[0].Eligible)
	assert.Equal(t, 10.0, eval.FinalPrice.Amount)
	assert.Equal(t, "USD", eval.FinalPrice.Currency)
}

func TestApplyManualDiscount_UnknownType(t *testing.T) {
	eval := baseEval()
	md := &repo.ManualDiscount{ID: 5, Type: "unknown_type"}
	applyManualDiscount(context.Background(), eval, md)

	require.Len(t, eval.Discounts, 1)
	assert.False(t, eval.Discounts[0].Eligible)
	assert.Equal(t, 80.0, eval.FinalPrice.Amount, "final price unchanged for unknown type")
}

func TestApplyManualDiscount_PercentMissingPct(t *testing.T) {
	eval := baseEval()
	props, _ := json.Marshal(map[string]interface{}{}) // no discount_pct field
	md := &repo.ManualDiscount{ID: 6, Type: "percent", Properties: null.JSONFrom(props)}
	applyManualDiscount(context.Background(), eval, md)

	require.Len(t, eval.Discounts, 1)
	assert.False(t, eval.Discounts[0].Eligible, "ineligible when discount_pct is nil")
	assert.Equal(t, 80.0, eval.FinalPrice.Amount)
}

func TestApplyManualDiscount_InvalidJSON(t *testing.T) {
	eval := baseEval()
	md := &repo.ManualDiscount{ID: 7, Type: "percent", Properties: null.JSONFrom([]byte("{invalid json}"))}
	applyManualDiscount(context.Background(), eval, md)

	require.Len(t, eval.Discounts, 1)
	assert.False(t, eval.Discounts[0].Eligible, "ineligible when properties JSON is invalid")
	assert.Equal(t, 80.0, eval.FinalPrice.Amount)
}

func TestApplyManualDiscount_PercentEligible_AppendsExplain(t *testing.T) {
	eval := baseEval()
	eval.Explain = []string{"step 1"}
	applyManualDiscount(context.Background(), eval, pctDiscount(9, 50))

	require.Len(t, eval.Discounts, 1)
	assert.True(t, eval.Discounts[0].Eligible)
	require.Len(t, eval.Explain, 2)
	assert.Contains(t, eval.Explain[1], "manual_discount")
	assert.Contains(t, eval.Explain[1], "applied")
}

func TestApplyManualDiscount_PercentNotEligible_NoExplainAppended(t *testing.T) {
	eval := baseEval()
	eval.FinalPrice = Price{Amount: 30.0, Currency: "NIS"} // already cheaper than any pct
	eval.Explain = []string{"step 1"}
	applyManualDiscount(context.Background(), eval, pctDiscount(10, 10)) // 80*90% = 72 > 30

	require.Len(t, eval.Discounts, 1)
	assert.False(t, eval.Discounts[0].Eligible)
	assert.Len(t, eval.Explain, 1, "explain unchanged when discount not applied")
}

func TestApplyManualDiscount_AuditPropertiesPopulated(t *testing.T) {
	eval := baseEval()
	applyManualDiscount(context.Background(), eval, pctDiscount(8, 25))

	require.Len(t, eval.Discounts, 1)
	var props map[string]interface{}
	require.NoError(t, json.Unmarshal(eval.Discounts[0].Properties, &props))
	assert.Equal(t, float64(8), props["manual_discount_id"])
	assert.Equal(t, "percent", props["original_type"])
	assert.Equal(t, 25.0, props["discount_pct"])
}
