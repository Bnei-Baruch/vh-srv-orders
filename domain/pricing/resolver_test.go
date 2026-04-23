package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func TestResolve_V1ForExcludedCountry(t *testing.T) {
	r := NewPriceResolver(nil, nil)
	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("US"),
		UserKey: null.StringFrom("kc1"),
		Email:   null.StringFrom("user@example.com"),
	}
	result, err := r.Resolve(context.Background(), account, common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, "v1", result.PricingVersion)
	assert.Equal(t, 20.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
	assert.Nil(t, result.V2Evaluation)
}

func TestResolve_V1FallsBackToUSDForUnknownCurrency(t *testing.T) {
	r := NewPriceResolver(nil, nil)
	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("GB"),
		Email:   null.StringFrom("user@example.com"),
	}
	result, err := r.Resolve(context.Background(), account, "GBP")
	require.NoError(t, err)
	assert.Equal(t, 20.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}

func TestResolve_V1NIS(t *testing.T) {
	r := NewPriceResolver(nil, nil)
	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("DE"), // EU excluded
		Email:   null.StringFrom("user@example.com"),
	}
	result, err := r.Resolve(context.Background(), account, common.CurrencyNIS)
	require.NoError(t, err)
	assert.Equal(t, 80.0, result.Amount)
	assert.Equal(t, common.CurrencyNIS, result.Currency)
}

func TestResolve_V2ErrorWhenPriorityNotConfigured(t *testing.T) {
	r := NewPriceResolver(nil, nil)
	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("IL"), // v2 eligible
		UserKey: null.StringFrom("kc1"),
		Email:   null.StringFrom("user@example.com"),
	}
	_, err := r.Resolve(context.Background(), account, common.CurrencyNIS)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
}
