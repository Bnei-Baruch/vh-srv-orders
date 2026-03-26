package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// stubAccounts satisfies AccountRepository for tests that don't reach v2 evaluation.
type stubAccounts struct{}

func (stubAccounts) GetAccountIDByKeycloakID(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (stubAccounts) GetEmailByKeycloakID(_ context.Context, _ string) (string, error) {
	return "", nil
}

func TestGetMonthlyPrice_V1_USD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "US", common.CurrencyUSD, "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
	assert.Equal(t, "v1", res.PricingVersion.String)
	assert.Nil(t, res.V2Details)
}

func TestGetMonthlyPrice_V1_EUR(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "DE", common.CurrencyEUR, "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyEUR, res.Currency.String)
}

func TestGetMonthlyPrice_V1_NIS(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "IL", common.CurrencyNIS, "v1")
	require.NoError(t, err)
	assert.Equal(t, 80.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyNIS, res.Currency.String)
}

func TestGetMonthlyPrice_V1_UnknownCurrencyFallsBackToUSD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "US", "GBP", "v1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
}

func TestGetMonthlyPrice_EmptyCurrencyDefaultsToUSD(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "US", "", "v1")
	require.NoError(t, err)
	assert.Equal(t, common.CurrencyUSD, res.Currency.String)
}

func TestGetMonthlyPrice_UnknownVersionFallsBackToV1(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "US", common.CurrencyUSD, "unknown")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Nil(t, res.V2Details)
}

func TestGetMonthlyPrice_T1_NonILNonNISUsesV1(t *testing.T) {
	res, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "US", common.CurrencyUSD, "t1")
	require.NoError(t, err)
	assert.Equal(t, 20.0, res.Amount.Float64)
	assert.Nil(t, res.V2Details)
}

func TestGetMonthlyPrice_V2_ErrorsWhenPriorityNotConfigured(t *testing.T) {
	_, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "IL", common.CurrencyNIS, "v2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
}

func TestGetMonthlyPrice_T1_ILErrorsWhenPriorityNotConfigured(t *testing.T) {
	_, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "IL", common.CurrencyUSD, "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
}

func TestGetMonthlyPrice_T1_NISCurrencyErrorsWhenPriorityNotConfigured(t *testing.T) {
	_, err := GetMonthlyPrice(context.Background(), stubAccounts{}, nil, nil,
		1, "kc1", "user@example.com", "US", common.CurrencyNIS, "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
}
