package pricing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

// --- helpers ---

func pct(n int) *int { return &n }

func futureGrant() time.Time { return time.Now().Add(30 * 24 * time.Hour) }
func pastGrant() time.Time   { return time.Now().Add(-time.Hour) }

// --- HHGrant helpers ---

func TestHHGrant_IsExpired_WhenPast(t *testing.T) {
	hh := &profiles.HHGrant{ExpiresAt: pastGrant()}
	assert.True(t, hh.IsExpired())
}

func TestHHGrant_IsExpired_WhenFuture(t *testing.T) {
	hh := &profiles.HHGrant{ExpiresAt: futureGrant()}
	assert.False(t, hh.IsExpired())
}

func TestHHGrant_IsFree_100Pct(t *testing.T) {
	hh := &profiles.HHGrant{DiscountPct: pct(100), ExpiresAt: futureGrant()}
	assert.True(t, hh.IsFree())
}


func TestHHGrant_IsFree_50Pct(t *testing.T) {
	hh := &profiles.HHGrant{DiscountPct: pct(50), ExpiresAt: futureGrant()}
	assert.False(t, hh.IsFree())
}

// --- resolveFinalPrice ---

func TestResolveFinalPrice_NoDiscounts_ReturnsBase(t *testing.T) {
	base := Price{Amount: 10, Currency: common.CurrencyUSD}
	result := resolveFinalPrice(nil, base, USDToNIS, EURToNIS)
	assert.Equal(t, base, result)
}

func TestResolveFinalPrice_IneligibleDiscount_ReturnsBase(t *testing.T) {
	base := Price{Amount: 10, Currency: common.CurrencyUSD}
	discounts := []Discount{{Type: DiscountTypeHH, AmountPct: 80, Eligible: false}}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, base, result)
	assert.False(t, discounts[0].Applied)
}

func TestResolveFinalPrice_PctDiscount_Wins(t *testing.T) {
	base := Price{Amount: 10, Currency: common.CurrencyUSD}
	discounts := []Discount{{Type: DiscountTypeHH, AmountPct: 50, Eligible: true}}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, 5.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
	assert.True(t, discounts[0].Applied)
}


func TestResolveFinalPrice_TwoDiscounts_LowerOneWins(t *testing.T) {
	// donations 55% off $55 → $24.75; HH 80% off $55 → $11 → HH wins
	base := Price{Amount: 55, Currency: common.CurrencyUSD}
	discounts := []Discount{
		{Type: DiscountTypeDonations, AmountPct: 55, Eligible: true},
		{Type: DiscountTypeHH, AmountPct: 80, Eligible: true},
	}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, 11.0, result.Amount)
	assert.False(t, discounts[0].Applied)
	assert.True(t, discounts[1].Applied)
}

func TestResolveFinalPrice_TwoDiscounts_DonationsWin(t *testing.T) {
	// donations 55% off $55 → $24.75; HH 20% off $55 → $44 → donations win
	base := Price{Amount: 55, Currency: common.CurrencyUSD}
	discounts := []Discount{
		{Type: DiscountTypeDonations, AmountPct: 55, Eligible: true},
		{Type: DiscountTypeHH, AmountPct: 20, Eligible: true},
	}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, 24.75, result.Amount)
	assert.True(t, discounts[0].Applied)
	assert.False(t, discounts[1].Applied)
}

func TestResolveFinalPrice_IneligibleIgnored_EligibleWins(t *testing.T) {
	// Ineligible 100% off, eligible 55% off → eligible wins (ineligible is skipped)
	base := Price{Amount: 10, Currency: common.CurrencyUSD}
	discounts := []Discount{
		{Type: DiscountTypeHH, AmountPct: 100, Eligible: false},
		{Type: DiscountTypeDonations, AmountPct: 55, Eligible: true},
	}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, 4.5, result.Amount)
	assert.False(t, discounts[0].Applied)
	assert.True(t, discounts[1].Applied)
}

func TestResolveFinalPrice_RoundsToTwoDecimals(t *testing.T) {
	// $12.50 * 55% off = $12.50 * 0.45 = $5.625 → rounds to $5.63
	base := Price{Amount: 12.50, Currency: common.CurrencyUSD}
	discounts := []Discount{{Type: DiscountTypeHH, AmountPct: 55, Eligible: true}}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, 5.63, result.Amount)
}


func TestResolveFinalPrice_100PctGivesFreePrice(t *testing.T) {
	base := Price{Amount: 20, Currency: common.CurrencyUSD}
	discounts := []Discount{{Type: DiscountTypeHH, AmountPct: 100, Eligible: true}}
	result := resolveFinalPrice(discounts, base, USDToNIS, EURToNIS)
	assert.Equal(t, 0.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
	assert.True(t, discounts[0].Applied)
}
