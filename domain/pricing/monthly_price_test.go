package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func TestGetMonthlyPrice_V1_USD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
		1, "kc1", "user@example.com", "US", common.CurrencyUSD, "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
	assert.Equal(t, "v1", res.PricingVersion.String)
	assert.Nil(t, res.V2Details)
}

func TestGetMonthlyPrice_V1_EUR(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
		1, "kc1", "user@example.com", "DE", common.CurrencyEUR, "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyEUR, res.Currency.String)
}

func TestGetMonthlyPrice_V1_NIS(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
		1, "kc1", "user@example.com", "IL", common.CurrencyNIS, "v1")
	require.NoError(t, err)
	assert.Equal(t, 80.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyNIS, res.Currency.String)
}

func TestGetMonthlyPrice_V1_UnknownCurrencyFallsBackToUSD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
		1, "kc1", "user@example.com", "US", "GBP", "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
}

func TestGetMonthlyPrice_EmptyCurrencyDefaultsToUSD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
		1, "kc1", "user@example.com", "US", "", "v1")
	require.NoError(t, err)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
}

func TestGetMonthlyPrice_V2_ErrorsWhenPriorityNotConfigured(t *testing.T) {
	orig := common.Config.PriorityBaseURL
	common.Config.PriorityBaseURL = ""
	t.Cleanup(func() { common.Config.PriorityBaseURL = orig })

	_, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
		1, "kc1", "user@example.com", "IL", common.CurrencyNIS, "v2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
}

func TestGetMonthlyPrice_DefaultRouteExcludedCountryGetsV1(t *testing.T) {
	// US is excluded from v2 — auto-route should return v1 regardless of input version label.
	for _, version := range []string{"", "unknown"} {
		res, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
			1, "kc1", "user@example.com", "US", common.CurrencyUSD, version)
		require.NoError(t, err, "version=%q", version)
		assert.Equal(t, 20.0, res.Amount.Float64, "version=%q", version)
		assert.Equal(t, "v1", res.PricingVersion.String, "version=%q", version)
		assert.Nil(t, res.V2Details, "version=%q", version)
	}
}

func TestGetMonthlyPrice_DefaultRouteEligibleCountryUsesV2(t *testing.T) {
	// IL is v2-eligible — auto-route should attempt v2 (fails without Priority configured).
	orig := common.Config.PriorityBaseURL
	common.Config.PriorityBaseURL = ""
	t.Cleanup(func() { common.Config.PriorityBaseURL = orig })

	for _, version := range []string{"", "unknown"} {
		_, err := GetMonthlyPrice(context.Background(), &stubProfileService{}, nil,
			1, "kc1", "user@example.com", "IL", common.CurrencyUSD, version)
		require.Error(t, err, "version=%q", version)
		assert.Contains(t, err.Error(), "PRIORITY_BASE_URL", "version=%q", version)
	}
}
