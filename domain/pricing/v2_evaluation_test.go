package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

// --- toNIS ---

func TestToNIS_NIS(t *testing.T) {
	assert.Equal(t, 100.0, toNIS(100.0, common.CurrencyNIS, 3.1, 3.6))
}

func TestToNIS_USD(t *testing.T) {
	assert.Equal(t, 310.0, toNIS(100.0, common.CurrencyUSD, 3.1, 3.6))
}

func TestToNIS_EUR(t *testing.T) {
	assert.Equal(t, 360.0, toNIS(100.0, common.CurrencyEUR, 3.1, 3.6))
}

func TestToNIS_UnknownCurrency(t *testing.T) {
	// Unknown currencies fall back to usdRate
	assert.Equal(t, 310.0, toNIS(100.0, "GBP", 3.1, 3.6))
}

// --- nisBase / unmarshalDonationsProps (used by EvaluateV2Price and Public tests) ---

// nisBase returns a NIS-denominated CountryBasePrice for testing.
func nisBase(amount float64) CountryBasePrice {
	return CountryBasePrice{Price: Price{Amount: amount, Currency: common.CurrencyNIS}, Group: "High"}
}

// unmarshalDonationsProps is a test helper that unmarshals the donations discount properties.
func unmarshalDonationsProps(t *testing.T, d Discount) DonationsDiscountProperties {
	t.Helper()
	var props DonationsDiscountProperties
	require.NoError(t, json.Unmarshal(d.Properties, &props))
	return props
}

// newPriorityTestClient creates a priority.Client pointed at the given test server URL.
func newPriorityTestClient(serverURL string) *priority.Client {
	common.Config.PriorityBaseURL = serverURL
	return priority.NewClient()
}

// --- Public test helpers ---

// baseProps returns a minimal DonationsDiscountProperties for formula tests.
func baseProps(primaryEmailCount int) DonationsDiscountProperties {
	return DonationsDiscountProperties{
		PrimaryEmailCount: primaryEmailCount,
		AnnualBase:        Price{Amount: 180 * 12, Currency: common.CurrencyNIS},
	}
}

// makeEval builds a V2PricingEvaluation for formula tests.
func makeEval(base CountryBasePrice, accountID int, finalPrice Price, props DonationsDiscountProperties, eligible bool) *V2PricingEvaluation {
	propsJSON, _ := json.Marshal(props)
	return &V2PricingEvaluation{
		EvaluatedAt: time.Now(),
		AccountID:   accountID,
		CountryCode: "IL",
		CountryBase: base,
		FinalPrice:  finalPrice,
		Discounts: []Discount{{
			Type:       DiscountTypeDonations,
			AmountPct:  50.0,
			Eligible:   eligible,
			Properties: propsJSON,
		}},
	}
}

// --- EvaluateV2Price ---

// stubProfileService is a minimal ProfileService for testing.
type stubProfileService struct {
	profiles    map[string]*profiles.Profile
	activeGrant *profiles.HHGrant // returned by GetActiveHHGrant for any keycloak ID
}

func (s *stubProfileService) GetProfileByKeycloakID(_ context.Context, kcID string) (*profiles.Profile, error) {
	if p, ok := s.profiles[kcID]; ok {
		return p, nil
	}
	return nil, profiles.ErrNotFound
}

func (s *stubProfileService) LookupProfile(context.Context, string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}

func (s *stubProfileService) LookupProfileByKeycloakId(context.Context, string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}

func (s *stubProfileService) GetActiveHHGrant(_ context.Context, _ string) (*profiles.HHGrant, error) {
	return s.activeGrant, nil
}

// priorityServerNoContributions returns a test server where all emails have active customers but zero qualifying contributions.
func priorityServerNoContributions() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "CUSTOMERS") {
			json.NewEncoder(w).Encode(priority.CustomerODataResponse{
				Value: []priority.Customer{{CustName: "CUST001", Email: "test@example.com"}},
			})
		} else {
			// No qualifying receivables
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{Value: []priority.AccountReceivableItem{}})
		}
	}))
}

// priorityServerWithContributions returns a test server where all emails have large NIS contributions (above annual threshold).
func priorityServerWithContributions(amount float64) *httptest.Server {
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "CUSTOMERS") {
			json.NewEncoder(w).Encode(priority.CustomerODataResponse{
				Value: []priority.Customer{{CustName: "CUST001"}},
			})
		} else {
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{
				Value: []priority.AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: amount, CODE: common.CurrencyNIS, FNCDATE: validDate},
				},
			})
		}
	}))
}

func TestEvaluateV2Price_NoSpouse_NoDiscount(t *testing.T) {
	server := priorityServerNoContributions()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &email},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.NoError(t, err)

	assert.Equal(t, 10, eval.AccountID)
	assert.Equal(t, "IL", eval.CountryCode)
	assert.False(t, eval.Discounts[0].Eligible)
	assert.Equal(t, eval.CountryBase.Amount, eval.FinalPrice.Amount)
}

func TestEvaluateV2Price_NoSpouse_WithDiscount(t *testing.T) {
	// Single person threshold = annual (base × 12); provide contributions above it.
	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100)
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &email},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.NoError(t, err)

	assert.True(t, eval.Discounts[0].Eligible)
	assert.Equal(t, base.Amount*(1-DonationsDiscountAmtPct/100), eval.FinalPrice.Amount, "should get 55% discount")
}

func TestEvaluateV2Price_ProfileNotFound_FallsBackToAccountEmail(t *testing.T) {
	server := priorityServerNoContributions()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	// No profile in stub → ErrNotFound → fallback to account email
	profileSvc := &stubProfileService{profiles: map[string]*profiles.Profile{}}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", "fallback@x.com", "IL")
	require.NoError(t, err)
	assert.NotNil(t, eval)
	assert.Equal(t, "IL", eval.CountryCode)
}

func TestEvaluateV2Price_WithSpouse_NeitherGetsDiscount(t *testing.T) {
	// Couple threshold = 2×annual. Each email contributes annual/2 → total = annual < 2×annual.
	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount * 12 / 2)
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	primaryEmail := "primary@x.com"
	spouseKC := "kc-spouse"
	spouseEmail := "spouse@x.com"

	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1":   {PrimaryEmail: &primaryEmail, SpouseKeycloakID: &spouseKC},
			spouseKC: {PrimaryEmail: &spouseEmail},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", primaryEmail, "IL")
	require.NoError(t, err)

	assert.False(t, eval.Discounts[0].Eligible)
	assert.Equal(t, base.Amount, eval.FinalPrice.Amount, "primary should pay full price")
	props := unmarshalDonationsProps(t, eval.Discounts[0])
	assert.False(t, props.SpouseGetsDiscount)
}

func TestEvaluateV2Price_WithSpouse_BothGetDiscount(t *testing.T) {
	// Each email contributes annual+50; 2 emails → total = 2×annual+100 >= 2×annual.
	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 50)
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	primaryEmail := "primary@x.com"
	spouseKC := "kc-spouse"
	spouseEmail := "spouse@x.com"

	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1":   {PrimaryEmail: &primaryEmail, SpouseKeycloakID: &spouseKC},
			spouseKC: {PrimaryEmail: &spouseEmail},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", primaryEmail, "IL")
	require.NoError(t, err)

	assert.True(t, eval.Discounts[0].Eligible)
	assert.Equal(t, base.Amount*(1-DonationsDiscountAmtPct/100), eval.FinalPrice.Amount, "primary should get discount")
	props := unmarshalDonationsProps(t, eval.Discounts[0])
	assert.True(t, props.SpouseGetsDiscount)
}

func TestEvaluateV2Price_FinalPriceRoundedToTwoDecimals(t *testing.T) {
	// $12.50 * 45% = $5.625 — three decimal places, requires rounding to $5.63.
	// Without math.Round in EvaluateV2Price, FinalPrice.Amount = 5.625 and this test fails.
	key := common.CurrencyUSD + "-High"
	original := priceByCurrencyAndGroup[key]
	priceByCurrencyAndGroup[key] = 12.50
	defer func() { priceByCurrencyAndGroup[key] = original }()

	base := GetCountryBasePrice("AU")
	annualThresholdNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS) * 12
	server := priorityServerWithContributions(annualThresholdNIS + 100)
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &email},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "AU")
	require.NoError(t, err)

	assert.True(t, eval.Discounts[0].Eligible)
	assert.Equal(t, 5.63, eval.FinalPrice.Amount, "$12.50 * 45%% = $5.625 must round to $5.63")
}

func TestEvaluateV2Price_SpouseDonationsCountedWithoutProfile(t *testing.T) {
	// Spouse keycloak ID is set but spouse has no profile (ErrNotFound).
	// Primary alone has contributions above single-person threshold → primary gets discount.
	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100)
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	primaryEmail := "primary@x.com"
	spouseKC := "kc-spouse"

	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &primaryEmail, SpouseKeycloakID: &spouseKC},
			// no entry for spouseKC → ErrNotFound
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", primaryEmail, "IL")
	require.NoError(t, err)
	assert.NotNil(t, eval)
}

func TestEvaluateV2Price_ProfileServiceError_ReturnsError(t *testing.T) {
	server := priorityServerNoContributions()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	profileSvc := &errorProfileService{err: fmt.Errorf("connection refused")}

	_, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", "test@x.com", "IL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profileService.GetProfileByKeycloakID")
}

// errorProfileService always returns the given error.
type errorProfileService struct{ err error }

func (e *errorProfileService) GetProfileByKeycloakID(context.Context, string) (*profiles.Profile, error) {
	return nil, e.err
}
func (e *errorProfileService) LookupProfile(context.Context, string) (*profiles.Profile, error) {
	return nil, e.err
}
func (e *errorProfileService) LookupProfileByKeycloakId(context.Context, string) (*profiles.Profile, error) {
	return nil, e.err
}
func (e *errorProfileService) GetActiveHHGrant(context.Context, string) (*profiles.HHGrant, error) {
	return nil, nil
}

func TestEvaluateV2Price_SpouseProfileError_ReturnsError(t *testing.T) {
	server := priorityServerNoContributions()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	primaryEmail := "primary@x.com"
	spouseKC := "kc-spouse"

	profileSvc := &spouseErrorProfileService{
		primaryProfile: &profiles.Profile{PrimaryEmail: &primaryEmail, SpouseKeycloakID: &spouseKC},
		spouseErr:      fmt.Errorf("timeout"),
	}

	_, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", primaryEmail, "IL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spouse")
}

// spouseErrorProfileService returns a profile for primary but errors on spouse.
type spouseErrorProfileService struct {
	primaryProfile *profiles.Profile
	spouseErr      error
}

func (s *spouseErrorProfileService) GetProfileByKeycloakID(_ context.Context, kcID string) (*profiles.Profile, error) {
	if s.primaryProfile != nil && s.primaryProfile.SpouseKeycloakID != nil && kcID == *s.primaryProfile.SpouseKeycloakID {
		return nil, s.spouseErr
	}
	return s.primaryProfile, nil
}
func (s *spouseErrorProfileService) LookupProfile(context.Context, string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}
func (s *spouseErrorProfileService) LookupProfileByKeycloakId(context.Context, string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}
func (s *spouseErrorProfileService) GetActiveHHGrant(context.Context, string) (*profiles.HHGrant, error) {
	return nil, nil
}

func TestEvaluateV2Price_DonationFetchError_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &email},
		},
	}

	_, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
}

func TestEvaluateV2Price_PartialDonationFetchError_ReturnsError(t *testing.T) {
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			email := r.URL.Query().Get("$filter")
			if strings.Contains(email, "bad@x.com") {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "internal error")
				return
			}
			json.NewEncoder(w).Encode(priority.CustomerODataResponse{
				Value: []priority.Customer{{CustName: "CUST001"}},
			})
		} else {
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{
				Value: []priority.AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 1000, CODE: common.CurrencyNIS, FNCDATE: validDate},
				},
			})
		}
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	good := "good@x.com"
	bad := "bad@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &good, AlternateEmail1: &bad},
		},
	}

	_, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", good, "IL")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
}

// --- Public ---

func TestV2PricingEvaluation_Public_StripsExplainAndProperties(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	eval := makeEval(base, 101, Price{Amount: 90, Currency: common.CurrencyNIS}, props, true)
	eval.Explain = []string{"step 1", "step 2"}

	pub := eval.Public()

	assert.Nil(t, pub.Explain)
	require.Len(t, pub.Discounts, 1)
	assert.Nil(t, pub.Discounts[0].Properties)
	assert.Equal(t, eval.Discounts[0].Type, pub.Discounts[0].Type)
	assert.Equal(t, eval.Discounts[0].AmountPct, pub.Discounts[0].AmountPct)
	assert.Equal(t, eval.Discounts[0].Eligible, pub.Discounts[0].Eligible)
}

func TestV2PricingEvaluation_Public_DoesNotMutateOriginal(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	eval := makeEval(base, 101, Price{Amount: 90, Currency: common.CurrencyNIS}, props, true)
	eval.Explain = []string{"step 1"}

	_ = eval.Public()

	assert.NotNil(t, eval.Explain)
	assert.NotNil(t, eval.Discounts[0].Properties)
}

func TestV2PricingEvaluation_Public_PreservesNonSensitiveFields(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	eval := makeEval(base, 101, Price{Amount: 180, Currency: common.CurrencyNIS}, props, false)

	pub := eval.Public()

	assert.Equal(t, eval.AccountID, pub.AccountID)
	assert.Equal(t, eval.CountryCode, pub.CountryCode)
	assert.Equal(t, eval.CountryBase, pub.CountryBase)
	assert.Equal(t, eval.FinalPrice, pub.FinalPrice)
}
