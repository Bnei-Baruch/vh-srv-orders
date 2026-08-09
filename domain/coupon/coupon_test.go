package coupon

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrF(f float64) *float64 { return &f }
func ptrI(i int) *int         { return &i }
func ptrS(s string) *string   { return &s }

func TestValidate_Percent(t *testing.T) {
	base := Fields{Type: TypePercent, DiscountPct: ptrF(50), BenefitMonths: ptrI(3), RedeemUntil: time.Now(), MaxRedemptions: 25}
	require.NoError(t, Validate(base))

	for _, bad := range []float64{0, 100, -5} {
		f := base
		f.DiscountPct = ptrF(bad)
		assert.Error(t, Validate(f), "pct=%v must be rejected (never-free, 1..99)", bad)
	}

	f := base
	f.DiscountPct = nil
	assert.Error(t, Validate(f))

	for _, bad := range []int{0, -1} {
		f := base
		f.MaxRedemptions = bad
		assert.Error(t, Validate(f), "max_redemptions=%d must be rejected", bad)
	}
}

func TestValidate_FixedPrice(t *testing.T) {
	ok := Fields{Type: TypeFixedPrice, FixedPrice: ptrF(100), FixedCurrency: ptrS("NIS"),
		BenefitMonths: ptrI(3), RedeemUntil: time.Now(), MaxRedemptions: 25}
	require.NoError(t, Validate(ok))

	// Countries are optional for fixed_price; currency need not match any country
	// (the evaluation converts). Both of these are valid now.
	withDifferentCurrencyCountry := ok
	withDifferentCurrencyCountry.Countries = []string{"US"} // USD country, NIS coupon
	require.NoError(t, Validate(withDifferentCurrencyCountry))

	zero := ok
	zero.FixedPrice = ptrF(0)
	assert.Error(t, Validate(zero), "fixed_price must be > 0")

	noCur := ok
	noCur.FixedCurrency = nil
	assert.Error(t, Validate(noCur))
}

func TestValidate_BenefitMode(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 3, 0)

	// exactly one mode → ok (window)
	win := Fields{Type: TypePercent, DiscountPct: ptrF(50), BenefitStart: &start, BenefitEnd: &end, RedeemUntil: start, MaxRedemptions: 25}
	require.NoError(t, Validate(win))

	// both modes → error
	both := win
	both.BenefitMonths = ptrI(3)
	assert.Error(t, Validate(both))

	// neither mode → error
	neither := Fields{Type: TypePercent, DiscountPct: ptrF(50), RedeemUntil: start, MaxRedemptions: 25}
	assert.Error(t, Validate(neither))

	// mode A: redeem_until after benefit_end → error
	late := win
	late.RedeemUntil = end.AddDate(0, 0, 1)
	assert.Error(t, Validate(late))
}

func TestGenerateCode(t *testing.T) {
	code, err := GenerateCode("sukkot")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(code, "SUKKOT-"), "prefix uppercased: %s", code)
	suffix := strings.TrimPrefix(code, "SUKKOT-")
	assert.Len(t, suffix, codeSuffixLen)
	for _, r := range suffix {
		assert.Contains(t, codeAlphabet, string(r), "suffix uses only the unambiguous alphabet")
	}

	_, err = GenerateCode("  ")
	assert.Error(t, err, "empty prefix rejected")

	// Digits and hyphens are allowed after the first (letter) character.
	dated, err := GenerateCode("sukkot-2026")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(dated, "SUKKOT-2026-"), "dated prefix preserved: %s", dated)

	for _, bad := range []string{"SUKKOT 2026", "2026SUKKOT", "-SUKKOT", "abc!", "סוכות"} {
		_, err := GenerateCode(bad)
		assert.Error(t, err, "prefix %q must be rejected", bad)
	}

	// Overwhelmingly likely to differ across calls.
	other, _ := GenerateCode("sukkot")
	assert.NotEqual(t, code, other)
}

func TestBenefitWindow_OffsetColorsWholeCalendarMonths(t *testing.T) {
	// Redeeming a 5-month coupon on Jan 7 colors all of Jan through all of May:
	// [Jan 1, Jun 1) (design §4 example).
	now := time.Date(2026, 1, 7, 15, 30, 0, 0, time.UTC)
	months := 5
	start, end, err := BenefitWindow(&months, nil, nil, now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), end)
}

func TestBenefitWindow_FixedCopiesDates(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	gotStart, gotEnd, err := BenefitWindow(nil, &start, &end, time.Now())
	require.NoError(t, err)
	assert.Equal(t, start, gotStart)
	assert.Equal(t, end, gotEnd)
}
