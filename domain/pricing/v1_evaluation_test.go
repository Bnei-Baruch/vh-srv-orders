package pricing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

func TestEvaluateV1_NoHHGrant(t *testing.T) {
	profileSvc := &stubProfileService{}
	eval, err := evaluateV1(context.Background(), profileSvc, 42, "kc-1", common.CurrencyUSD)
	require.NoError(t, err)

	assert.Equal(t, 42, eval.AccountID)
	assert.Equal(t, Price{Amount: 20, Currency: common.CurrencyUSD}, eval.BasePrice)
	assert.Equal(t, Price{Amount: 20, Currency: common.CurrencyUSD}, eval.FinalPrice)
	assert.Empty(t, eval.Discounts)
	require.Len(t, eval.Explain, 2)
	assert.Equal(t, "base: 20.00 USD/mo", eval.Explain[0])
	assert.Equal(t, "final: primary[#42] 20.00 USD", eval.Explain[1])
}

func TestEvaluateV1_HHApplied(t *testing.T) {
	grant := &profiles.HHGrant{DiscountPct: pct(100), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	eval, err := evaluateV1(context.Background(), profileSvc, 7, "kc-1", common.CurrencyUSD)
	require.NoError(t, err)

	assert.Equal(t, Price{Amount: 0, Currency: common.CurrencyUSD}, eval.FinalPrice)
	require.Len(t, eval.Discounts, 1)
	assert.Equal(t, DiscountTypeHH, eval.Discounts[0].Type)
	assert.True(t, eval.Discounts[0].Applied)
	require.Len(t, eval.Explain, 3)
	assert.Equal(t, "base: 20.00 USD/mo", eval.Explain[0])
	assert.Contains(t, eval.Explain[1], "Help Haver")
	assert.Contains(t, eval.Explain[1], "applied")
	assert.Equal(t, "final: primary[#7] 0.00 USD", eval.Explain[2])
}

func TestEvaluateV1_HHPartialDiscount(t *testing.T) {
	grant := &profiles.HHGrant{DiscountPct: pct(80), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	eval, err := evaluateV1(context.Background(), profileSvc, 1, "kc-1", common.CurrencyUSD)
	require.NoError(t, err)

	assert.Equal(t, 4.0, eval.FinalPrice.Amount) // $20 * 0.20 = $4
	assert.True(t, eval.Discounts[0].Applied)
	assert.Contains(t, eval.Explain[1], "80%")
}

func TestEvaluateV1_HHExpired(t *testing.T) {
	grant := &profiles.HHGrant{DiscountPct: pct(100), ExpiresAt: time.Now().Add(-time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	eval, err := evaluateV1(context.Background(), profileSvc, 1, "kc-1", common.CurrencyUSD)
	require.NoError(t, err)

	assert.Equal(t, 20.0, eval.FinalPrice.Amount)
	assert.Empty(t, eval.Discounts)
	assert.Len(t, eval.Explain, 2) // no HH line
}

func TestEvaluateV1_NISCurrency(t *testing.T) {
	profileSvc := &stubProfileService{}
	eval, err := evaluateV1(context.Background(), profileSvc, 1, "kc-1", common.CurrencyNIS)
	require.NoError(t, err)

	assert.Equal(t, Price{Amount: 80, Currency: common.CurrencyNIS}, eval.BasePrice)
	assert.Equal(t, 80.0, eval.FinalPrice.Amount)
	assert.Equal(t, "base: 80.00 NIS/mo", eval.Explain[0])
}

func TestEvaluateV1_EmptyKeycloakID_ReturnsError(t *testing.T) {
	profileSvc := &stubProfileService{}
	_, err := evaluateV1(context.Background(), profileSvc, 1, "", common.CurrencyUSD)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keycloakID is required")
}

func TestV1PricingEvaluation_Public_StripsExplainAndProperties(t *testing.T) {
	grant := &profiles.HHGrant{DiscountPct: pct(50), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	eval, err := evaluateV1(context.Background(), profileSvc, 1, "kc-1", common.CurrencyUSD)
	require.NoError(t, err)
	require.NotEmpty(t, eval.Explain)
	require.NotEmpty(t, eval.Discounts[0].Properties)

	pub := eval.Public()
	assert.Nil(t, pub.Explain)
	assert.Nil(t, pub.Discounts[0].Properties)
	assert.Equal(t, DiscountTypeHH, pub.Discounts[0].Type)
	assert.True(t, pub.Discounts[0].Applied)
}

func TestV1PricingEvaluation_Public_DoesNotMutateOriginal(t *testing.T) {
	grant := &profiles.HHGrant{DiscountPct: pct(50), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	eval, err := evaluateV1(context.Background(), profileSvc, 1, "kc-1", common.CurrencyUSD)
	require.NoError(t, err)

	origExplain := eval.Explain
	origProps := eval.Discounts[0].Properties

	_ = eval.Public()

	assert.Equal(t, origExplain, eval.Explain)
	assert.Equal(t, origProps, eval.Discounts[0].Properties)
}
