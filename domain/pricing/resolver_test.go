package pricing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func TestResolve_V1ForExcludedCountry(t *testing.T) {
	r := NewPriceResolver(&stubProfileService{}, nil)
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
	r := NewPriceResolver(&stubProfileService{}, nil)
	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("GB"),
		UserKey: null.StringFrom("kc1"),
		Email:   null.StringFrom("user@example.com"),
	}
	result, err := r.Resolve(context.Background(), account, "GBP")
	require.NoError(t, err)
	assert.Equal(t, 20.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}

func TestResolve_V1NIS(t *testing.T) {
	r := NewPriceResolver(&stubProfileService{}, nil)
	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("DE"), // EU excluded
		UserKey: null.StringFrom("kc1"),
		Email:   null.StringFrom("user@example.com"),
	}
	result, err := r.Resolve(context.Background(), account, common.CurrencyNIS)
	require.NoError(t, err)
	assert.Equal(t, 80.0, result.Amount)
	assert.Equal(t, common.CurrencyNIS, result.Currency)
}

func TestResolve_V2ErrorWhenPriorityNotConfigured(t *testing.T) {
	orig := common.Config.PriorityBaseURL
	common.Config.PriorityBaseURL = ""
	t.Cleanup(func() { common.Config.PriorityBaseURL = orig })

	r := NewPriceResolver(&stubProfileService{}, nil)
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

// --- V1 + HH ---

func v1USDAccount() *repo.Account {
	return &repo.Account{
		ID:      1,
		Country: null.StringFrom("US"), // V1 (excluded from V2)
		UserKey: null.StringFrom("kc-1"),
		Email:   null.StringFrom("user@example.com"),
	}
}

func TestResolve_V1_NoHHGrant(t *testing.T) {
	profileSvc := &stubProfileService{}
	r := NewPriceResolver(profileSvc, nil)
	result, err := r.Resolve(context.Background(), v1USDAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, 20.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}

func TestResolve_V1_HHPctWins(t *testing.T) {
	// HH 80% off $20 → $4 (34.1% of V1 price in NIS → better than $20)
	grant := &profiles.HHGrant{DiscountPct: pct(80), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	r := NewPriceResolver(profileSvc, nil)
	result, err := r.Resolve(context.Background(), v1USDAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, 4.0, result.Amount) // $20 * 0.20 = $4
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}


func TestResolve_V1_HHExpired(t *testing.T) {
	// Expired grant → ignored, normal V1 price
	grant := &profiles.HHGrant{DiscountPct: pct(80), ExpiresAt: time.Now().Add(-time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	r := NewPriceResolver(profileSvc, nil)
	result, err := r.Resolve(context.Background(), v1USDAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, 20.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}

func TestResolve_V1_HH100Pct(t *testing.T) {
	// 100% discount → price drops to 0 (free)
	grant := &profiles.HHGrant{DiscountPct: pct(100), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{activeGrant: grant}
	r := NewPriceResolver(profileSvc, nil)
	result, err := r.Resolve(context.Background(), v1USDAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, 0.0, result.Amount)
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}

// --- V2 + HH ---

// resolverPriorityServer creates a test server that returns the given NIS contribution amount.
// Amount=0 means no qualifying contributions.
func resolverPriorityServer(nisContribution float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "CUSTOMERS") {
			json.NewEncoder(w).Encode(priority.CustomerODataResponse{
				Value: []priority.Customer{{CustName: "CUST001"}},
			})
		} else if nisContribution > 0 {
			validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{
				Value: []priority.AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: nisContribution, CODE: common.CurrencyNIS, FNCDATE: validDate},
				},
			})
		} else {
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{Value: []priority.AccountReceivableItem{}})
		}
	}))
}

func v2AUAccount() *repo.Account {
	return &repo.Account{
		ID:      10,
		Country: null.StringFrom("AU"), // V2-eligible, USD-High tier ($55/month)
		UserKey: null.StringFrom("kc-1"),
		Email:   null.StringFrom("user@example.com"),
	}
}

func TestResolve_V2_HHPctWins(t *testing.T) {
	// AU base $55; donations 55% off → $24.75 current.
	// HH 80% off base → $55 * 0.20 = $11 (34.1 NIS) < $24.75 (76.725 NIS) → HH wins.
	base := GetCountryBasePrice("AU")
	annualNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS) * 12
	server := resolverPriorityServer(annualNIS + 100) // above single-person threshold
	defer server.Close()

	email := "user@example.com"
	grant := &profiles.HHGrant{DiscountPct: pct(80), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{
		profiles:    map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
		activeGrant: grant,
	}
	common.Config.PriorityBaseURL = server.URL
	r := NewPriceResolver(profileSvc, priority.NewClient())

	result, err := r.Resolve(context.Background(), v2AUAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, "v2", result.PricingVersion)
	assert.Equal(t, 11.0, result.Amount) // $55 * 0.20
	assert.Equal(t, common.CurrencyUSD, result.Currency)
}

func TestResolve_V2_CurrentWins_DonationsBetterThanHH(t *testing.T) {
	// AU base $55; donations 55% off → $24.75 current.
	// HH 20% off base → $55 * 0.80 = $44 (136.4 NIS) > $24.75 (76.725 NIS) → donations win.
	base := GetCountryBasePrice("AU")
	annualNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS) * 12
	server := resolverPriorityServer(annualNIS + 100)
	defer server.Close()

	email := "user@example.com"
	grant := &profiles.HHGrant{DiscountPct: pct(20), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{
		profiles:    map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
		activeGrant: grant,
	}
	common.Config.PriorityBaseURL = server.URL
	r := NewPriceResolver(profileSvc, priority.NewClient())

	result, err := r.Resolve(context.Background(), v2AUAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, "v2", result.PricingVersion)
	donationsPrice := base.Amount * (1 - DonationsDiscountAmtPct/100)
	assert.Equal(t, donationsPrice, result.Amount) // $55 * 0.45 = $24.75
}

func TestResolve_V2_HHWins_NoDonationsDiscount(t *testing.T) {
	// AU base $55; no donations → current = $55.
	// HH 50% off base → $55 * 0.50 = $27.50 < $55 → HH wins.
	server := resolverPriorityServer(0) // below threshold → no donation discount
	defer server.Close()

	email := "user@example.com"
	grant := &profiles.HHGrant{DiscountPct: pct(50), ExpiresAt: time.Now().Add(time.Hour)}
	profileSvc := &stubProfileService{
		profiles:    map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
		activeGrant: grant,
	}
	common.Config.PriorityBaseURL = server.URL
	r := NewPriceResolver(profileSvc, priority.NewClient())

	result, err := r.Resolve(context.Background(), v2AUAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	assert.Equal(t, "v2", result.PricingVersion)
	assert.Equal(t, 27.5, result.Amount) // $55 * 0.50
}

func TestResolve_V2_HHExpired_NoDonations(t *testing.T) {
	// Expired HH grant → ignored; no donations → base price.
	server := resolverPriorityServer(0)
	defer server.Close()

	email := "user@example.com"
	grant := &profiles.HHGrant{DiscountPct: pct(80), ExpiresAt: time.Now().Add(-time.Hour)}
	profileSvc := &stubProfileService{
		profiles:    map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
		activeGrant: grant,
	}
	common.Config.PriorityBaseURL = server.URL
	r := NewPriceResolver(profileSvc, priority.NewClient())

	result, err := r.Resolve(context.Background(), v2AUAccount(), common.CurrencyUSD)
	require.NoError(t, err)
	base := GetCountryBasePrice("AU")
	assert.Equal(t, base.Amount, result.Amount)
}
