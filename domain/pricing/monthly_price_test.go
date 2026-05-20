package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func TestGetMonthlyPrice_V1_USD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), nil, nil, nil, "",
		1, "kc1", "user@example.com", "US", common.CurrencyUSD, "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
	assert.Equal(t, "v1", res.PricingVersion.String)
	assert.Nil(t, res.V2Details)
}

func TestGetMonthlyPrice_V1_EUR(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), nil, nil, nil, "",
		1, "kc1", "user@example.com", "DE", common.CurrencyEUR, "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyEUR, res.Currency.String)
}

func TestGetMonthlyPrice_V1_NIS(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), nil, nil, nil, "",
		1, "kc1", "user@example.com", "IL", common.CurrencyNIS, "v1")
	require.NoError(t, err)
	assert.Equal(t, 80.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyNIS, res.Currency.String)
}

func TestGetMonthlyPrice_V1_UnknownCurrencyFallsBackToUSD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), nil, nil, nil, "",
		1, "kc1", "user@example.com", "US", "GBP", "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
}

func TestGetMonthlyPrice_EmptyCurrencyDefaultsToUSD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), nil, nil, nil, "",
		1, "kc1", "user@example.com", "US", "", "v1")
	require.NoError(t, err)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
}

func TestGetMonthlyPrice_DefaultRouteExcludedCountryGetsV1(t *testing.T) {
	// GB is excluded from v2 — auto-route should return v1 regardless of input version label.
	for _, version := range []string{"", "t1", "unknown"} {
		res, err := GetMonthlyPrice(context.Background(), nil, nil, nil, "",
			1, "kc1", "user@example.com", "GB", common.CurrencyUSD, version)
		require.NoError(t, err, "version=%q", version)
		assert.Equal(t, 20.0, res.Amount.Float64, "version=%q", version)
		assert.Equal(t, "v1", res.PricingVersion.String, "version=%q", version)
		assert.Nil(t, res.V2Details, "version=%q", version)
	}
}
