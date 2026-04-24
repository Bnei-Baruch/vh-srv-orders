package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
)

// makeResolvedOrder builds a minimal resolvedOrder with a v2 evaluation
// whose discount list is supplied by the caller.
func makeResolvedOrder(baseAmount, finalAmount float64, currency string, discounts []pricing.Discount) resolvedOrder {
	return resolvedOrder{
		Price: &pricing.ChargePrice{
			Amount:         finalAmount,
			Currency:       currency,
			PricingVersion: "v2",
			V2Evaluation: &pricing.V2PricingEvaluation{
				CountryBase: pricing.CountryBasePrice{
					Price: pricing.Price{Amount: baseAmount, Currency: currency},
				},
				FinalPrice: pricing.Price{Amount: finalAmount, Currency: currency},
				Discounts:  discounts,
			},
		},
	}
}

func TestAggregateDiscountStats_AttributesAmountToAppliedOnly(t *testing.T) {
	// Order 1: both discounts Eligible, donations Applied, manual not.
	// Order 2: both discounts Eligible, manual Applied, donations not.
	// Each order saves 100 NIS (base=200, final=100). The savings must land
	// exactly once per order, in the bucket of the Applied discount — never
	// double-counted across the two buckets.
	orders := []resolvedOrder{
		makeResolvedOrder(200, 100, common.CurrencyNIS, []pricing.Discount{
			{Type: pricing.DiscountTypeDonations, AmountPct: 55, Eligible: true, Applied: true},
			{Type: pricing.DiscountTypeManual, AmountPct: 30, Eligible: true, Applied: false},
		}),
		makeResolvedOrder(200, 100, common.CurrencyNIS, []pricing.Discount{
			{Type: pricing.DiscountTypeDonations, AmountPct: 55, Eligible: true, Applied: false},
			{Type: pricing.DiscountTypeManual, AmountPct: 70, Eligible: true, Applied: true},
		}),
	}

	byType := aggregateDiscountStats(orders)

	donations, ok := byType[pricing.DiscountTypeDonations]
	require.True(t, ok)
	assert.Equal(t, 2, donations.eligibleOrders)
	assert.Equal(t, 0, donations.ineligibleOrders)
	assert.InDelta(t, 100.0, donations.amountByCurrency[common.CurrencyNIS], 1e-9,
		"donations bucket should only reflect the order where donations won")

	manual, ok := byType[pricing.DiscountTypeManual]
	require.True(t, ok)
	assert.Equal(t, 2, manual.eligibleOrders)
	assert.Equal(t, 0, manual.ineligibleOrders)
	assert.InDelta(t, 100.0, manual.amountByCurrency[common.CurrencyNIS], 1e-9,
		"manual bucket should only reflect the order where manual won")
}

func TestAggregateDiscountStats_IneligibleIsCountedNoAmount(t *testing.T) {
	// Donations Eligible but not Applied (manual beat it); donations still
	// contributes to eligibleOrders count, but zero to the amount bucket.
	orders := []resolvedOrder{
		makeResolvedOrder(200, 60, common.CurrencyNIS, []pricing.Discount{
			{Type: pricing.DiscountTypeDonations, AmountPct: 55, Eligible: true, Applied: false},
			{Type: pricing.DiscountTypeManual, AmountPct: 70, Eligible: true, Applied: true},
		}),
		makeResolvedOrder(200, 200, common.CurrencyNIS, []pricing.Discount{
			{Type: pricing.DiscountTypeDonations, AmountPct: 55, Eligible: false, Applied: false},
		}),
	}

	byType := aggregateDiscountStats(orders)

	donations := byType[pricing.DiscountTypeDonations]
	assert.Equal(t, 1, donations.eligibleOrders)
	assert.Equal(t, 1, donations.ineligibleOrders)
	assert.Zero(t, donations.amountByCurrency[common.CurrencyNIS])

	manual := byType[pricing.DiscountTypeManual]
	assert.Equal(t, 1, manual.eligibleOrders)
	assert.InDelta(t, 140.0, manual.amountByCurrency[common.CurrencyNIS], 1e-9)
}

func TestAggregateDiscountStats_SkipsV1Orders(t *testing.T) {
	v1Order := resolvedOrder{
		Price: &pricing.ChargePrice{
			Amount: 20, Currency: common.CurrencyUSD, PricingVersion: "v1",
			// V2Evaluation intentionally nil
		},
	}
	byType := aggregateDiscountStats([]resolvedOrder{v1Order})
	assert.Empty(t, byType)
}
