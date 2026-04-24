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

// --- deduplicateEmails ---

func TestDeduplicateEmails_BothEmpty(t *testing.T) {
	result := deduplicateEmails(nil, nil)
	assert.Empty(t, result)
}

func TestDeduplicateEmails_NoDuplicates(t *testing.T) {
	result := deduplicateEmails([]string{"a@x.com", "b@x.com"}, []string{"c@x.com"})
	assert.Equal(t, []string{"a@x.com", "b@x.com", "c@x.com"}, result)
}

func TestDeduplicateEmails_CrossSliceDuplicate(t *testing.T) {
	result := deduplicateEmails([]string{"a@x.com"}, []string{"a@x.com", "b@x.com"})
	assert.Equal(t, []string{"a@x.com", "b@x.com"}, result)
}

func TestDeduplicateEmails_CaseInsensitive(t *testing.T) {
	result := deduplicateEmails([]string{"A@X.com"}, []string{"a@x.com", "b@x.com"})
	// "a@x.com" is considered a duplicate of "A@X.com"; original casing from first slice is kept
	assert.Equal(t, []string{"A@X.com", "b@x.com"}, result)
}

// --- collectProfileEmails ---

func TestCollectProfileEmails_NilProfileUsesFallback(t *testing.T) {
	result := collectProfileEmails(nil, "fallback@x.com")
	assert.Equal(t, []string{"fallback@x.com"}, result)
}

func TestCollectProfileEmails_AllThreeProfileEmails(t *testing.T) {
	primary := "primary@x.com"
	alt1 := "alt1@x.com"
	alt2 := "alt2@x.com"
	profile := &profiles.Profile{
		PrimaryEmail:    &primary,
		AlternateEmail1: &alt1,
		AlternateEmail2: &alt2,
	}
	result := collectProfileEmails(profile, "fallback@x.com")
	assert.Equal(t, []string{"primary@x.com", "alt1@x.com", "alt2@x.com"}, result)
}

func TestCollectProfileEmails_FallbackIgnoredWhenProfileHasEmails(t *testing.T) {
	primary := "primary@x.com"
	profile := &profiles.Profile{PrimaryEmail: &primary}
	result := collectProfileEmails(profile, "fallback@x.com")
	assert.Equal(t, []string{"primary@x.com"}, result)
}

func TestCollectProfileEmails_DeduplicatesWithinProfile(t *testing.T) {
	addr := "same@x.com"
	profile := &profiles.Profile{
		PrimaryEmail:    &addr,
		AlternateEmail1: &addr,
	}
	result := collectProfileEmails(profile, "fallback@x.com")
	assert.Equal(t, []string{"same@x.com"}, result)
}

// --- buildDonationsDiscount ---

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

func TestBuildDonationsDiscount_WithSpouse_BelowDoubleAnnual_NeitherGets(t *testing.T) {
	// annual = 180*12 = 2160, 2×annual = 4320; combined = 3000 < 4320
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 3000}
	d := buildDonationsDiscount(sums, base, 180, 1, 1, "spouse-kc")
	assert.False(t, d.Eligible)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_WithSpouse_AboveDoubleAnnual_BothGet(t *testing.T) {
	// combined = 4400 >= 4320
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 4400}
	d := buildDonationsDiscount(sums, base, 180, 1, 1, "spouse-kc")
	assert.True(t, d.Eligible)
	props := unmarshalDonationsProps(t, d)
	assert.True(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_BelowAnnual_NoDiscount(t *testing.T) {
	// annual = 2160; combined = 1200 < 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 1200}
	d := buildDonationsDiscount(sums, base, 180, 1, 0, "")
	assert.False(t, d.Eligible)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_AboveAnnual_PrimaryGets(t *testing.T) {
	// combined = 2200 >= 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2200}
	d := buildDonationsDiscount(sums, base, 180, 1, 0, "")
	assert.True(t, d.Eligible)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_PropertiesStoredCorrectly(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{
		perCurrency:   map[string]float64{},
		totalNIS:      4400, // above 2×annual to make eligible
		successEmails: []string{"a@x.com"},
		fetchNote:     "some note",
	}
	d := buildDonationsDiscount(sums, base, 180, 2, 1, "spouse-kc-id")
	props := unmarshalDonationsProps(t, d)
	assert.Equal(t, 2, props.PrimaryEmailCount)
	assert.Equal(t, 1, props.SpouseEmailCount)
	assert.Equal(t, "spouse-kc-id", props.SpouseKeycloakID)
	assert.Equal(t, []string{"a@x.com"}, props.DonationsFetchedEmails)
	assert.Equal(t, "some note", props.DonationsFetchNote)
	assert.Equal(t, Price{Amount: 180 * 12, Currency: common.CurrencyNIS}, props.AnnualBase)
	assert.Equal(t, DiscountTypeDonations, d.Type)
	assert.Equal(t, 55.0, d.AmountPct)
}

// --- fetchDonationSums ---

// newPriorityTestClient creates a priority.Client pointed at the given test server URL.
func newPriorityTestClient(serverURL string) *priority.Client {
	common.Config.PriorityBaseURL = serverURL
	return priority.NewClient()
}

func TestFetchDonationSums_NoAccount_TreatedAsZero(t *testing.T) {
	// Empty customers list → GetLastContributions returns "no active customers found for email: ..."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(priority.CustomerODataResponse{Value: []priority.Customer{}})
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	result, err := fetchDonationSums(context.Background(), client, []string{"unknown@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.Contains(t, result.fetchNote, "unknown@x.com") // recorded as "no Priority account"
	assert.Equal(t, 0.0, result.totalNIS)
}

func TestFetchDonationSums_APIError_ReturnsError(t *testing.T) {
	// Real API error (not "customer not found") on first email → fail immediately
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	_, err := fetchDonationSums(context.Background(), client, []string{"bad@x.com"}, 3.1, 3.6)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
	assert.Contains(t, err.Error(), "bad@x.com")
}

func TestFetchDonationSums_PartialAPIError_ReturnsError(t *testing.T) {
	// Good email succeeds, bad email errors → still returns error (fail-fast on second)
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
					{ACCNAME: "40001", DEBIT: 100, CODE: common.CurrencyNIS, FNCDATE: validDate},
				},
			})
		}
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	// good@x.com first (succeeds), bad@x.com second (errors) — result must still be an error
	_, err := fetchDonationSums(context.Background(), client, []string{"good@x.com", "bad@x.com"}, 3.1, 3.6)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
	assert.Contains(t, err.Error(), "bad@x.com")
}

func TestFetchDonationSums_AggregatesAcrossEmails(t *testing.T) {
	// Two emails: first has 100 USD, second has 200 NIS
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			email := r.URL.Query().Get("$filter")
			if strings.Contains(email, "usd@x.com") {
				json.NewEncoder(w).Encode(priority.CustomerODataResponse{
					Value: []priority.Customer{{CustName: "CUST_USD"}},
				})
			} else {
				json.NewEncoder(w).Encode(priority.CustomerODataResponse{
					Value: []priority.Customer{{CustName: "CUST_NIS"}},
				})
			}
		} else if strings.Contains(r.URL.Path, "CUST_USD") {
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{
				Value: []priority.AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 100, CODE: common.CurrencyUSD, FNCDATE: validDate},
				},
			})
		} else {
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{
				Value: []priority.AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 200, CODE: common.CurrencyNIS, FNCDATE: validDate},
				},
			})
		}
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	// usdRate=3.1: 100 USD = 310 NIS; plus 200 NIS = 510 NIS total
	result, err := fetchDonationSums(context.Background(), client, []string{"usd@x.com", "nis@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.Empty(t, result.fetchNote)
	assert.InDelta(t, 510.0, result.totalNIS, 0.001)
}

// --- buildExplain ---

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

func TestBuildFormula_NoSpouseNoDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	eval := makeEval(base, 101, Price{Amount: 180, Currency: common.CurrencyNIS}, props, false)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[0], "IL")
	assert.Contains(t, lines[0], "180.00 NIS/mo")
	assert.Contains(t, lines[0], "2160.00 NIS/yr")
	assert.NotContains(t, lines[0], "@ 1")
	assert.Contains(t, lines[1], "primary(2)")
	assert.Contains(t, lines[1], "ok")
	assert.Equal(t, "3. sum all donations per currency → convert each to NIS", lines[2])
	assert.Contains(t, lines[3], "not eligible")
	assert.Contains(t, lines[3], "2160.00 NIS/yr") // single-person threshold = annual
	assert.NotContains(t, lines[3], "→ NIS")
	assert.Contains(t, lines[4], "#101")
	assert.Contains(t, lines[4], "180.00 NIS")
}

func TestBuildFormula_NoSpousePrimaryDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	discounted := base.Amount * (1 - DonationsDiscountAmtPct/100)
	eval := makeEval(base, 101, Price{Amount: discounted, Currency: common.CurrencyNIS}, props, true)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[3], "primary eligible for 55% off")
	assert.Contains(t, lines[3], "2160.00 NIS/yr") // single-person threshold = annual
	assert.Contains(t, lines[4], "81.00 NIS")
}

func TestBuildFormula_WithSpouseBothDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	props.SpouseKeycloakID = "kc-spouse"
	props.SpouseEmailCount = 1
	props.SpouseGetsDiscount = true
	discounted := base.Amount * (1 - DonationsDiscountAmtPct/100)
	eval := makeEval(base, 101, Price{Amount: discounted, Currency: common.CurrencyNIS}, props, true)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[1], "primary(2) + spouse(1) = 3 unique")
	assert.Contains(t, lines[3], "both members eligible for 55% off")
	assert.Contains(t, lines[3], "4320.00 NIS/yr") // 2×annual threshold
	assert.Contains(t, lines[4], "#101")
	assert.Contains(t, lines[4], "spouse:")
}

func TestBuildFormula_WithSpouseNeitherGetsDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2)
	props.SpouseKeycloakID = "kc-spouse"
	props.SpouseEmailCount = 1
	props.SpouseGetsDiscount = false
	eval := makeEval(base, 101, Price{Amount: 180, Currency: common.CurrencyNIS}, props, false)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[3], "not eligible")
	assert.Contains(t, lines[3], "4320.00 NIS/yr") // 2×annual threshold shown
}

func TestBuildFormula_ForeignCurrencyOriginalCurrency(t *testing.T) {
	base := CountryBasePrice{Price: Price{Amount: 10, Currency: common.CurrencyUSD}, Group: "Medium"}
	props := DonationsDiscountProperties{
		PrimaryEmailCount: 1,
		AnnualBase:        Price{Amount: 10 * 12, Currency: common.CurrencyUSD},
	}
	eval := makeEval(base, 1, Price{Amount: 10, Currency: common.CurrencyUSD}, props, false)
	eval.CountryCode = "US"

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	// Step 1: original currency only, no NIS conversion
	assert.Contains(t, lines[0], "10.00 USD/mo")
	assert.Contains(t, lines[0], "120.00 USD/yr")
	assert.NotContains(t, lines[0], "NIS")
	assert.NotContains(t, lines[0], "@ 1")
	// Step 3: acknowledges NIS conversion without showing amounts
	assert.Equal(t, "3. sum all donations per currency → convert each to NIS", lines[2])
	// Step 4: single-person threshold = annual (120 USD) with → NIS marker
	assert.Contains(t, lines[3], "120.00 USD/yr (→ NIS)")
	assert.NotContains(t, lines[3], "744") // 2×annual in NIS, must not appear
}

// --- EvaluateV2Price ---

// stubProfileService is a minimal ProfileService for testing.
type stubProfileService struct {
	profiles map[string]*profiles.Profile
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
	assert.InDelta(t, base.Amount*(1-DonationsDiscountAmtPct/100), eval.FinalPrice.Amount, 1e-9, "should get 55% discount")
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
	assert.InDelta(t, base.Amount*(1-DonationsDiscountAmtPct/100), eval.FinalPrice.Amount, 1e-9, "primary should get discount")
	props := unmarshalDonationsProps(t, eval.Discounts[0])
	assert.True(t, props.SpouseGetsDiscount)
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

// --- Manual discount ---

// setManualDiscountForTest registers a manual discount entry for the duration
// of the test and cleans it up afterwards. Not safe for t.Parallel with the
// same keycloak ID.
func setManualDiscountForTest(t *testing.T, keycloakID string, pct float64) {
	t.Helper()
	manualDiscounts[keycloakID] = pct
	t.Cleanup(func() { delete(manualDiscounts, keycloakID) })
}

func TestLookupManualDiscount_Hit(t *testing.T) {
	setManualDiscountForTest(t, "kc-hit", 40.0)
	pct, ok := lookupManualDiscount("kc-hit")
	assert.True(t, ok)
	assert.Equal(t, 40.0, pct)
}

func TestLookupManualDiscount_Miss(t *testing.T) {
	pct, ok := lookupManualDiscount("kc-unknown")
	assert.False(t, ok)
	assert.Equal(t, 0.0, pct)
}

func TestLookupManualDiscount_EmptyKey(t *testing.T) {
	pct, ok := lookupManualDiscount("")
	assert.False(t, ok)
	assert.Equal(t, 0.0, pct)
}

// findDiscount returns the first discount of the given type in the slice.
func findDiscount(discounts []Discount, t DiscountType) (Discount, bool) {
	for _, d := range discounts {
		if d.Type == t {
			return d, true
		}
	}
	return Discount{}, false
}

func TestEvaluateV2Price_ManualOnly_NotEligibleDonations(t *testing.T) {
	// No donations → manual 30% wins.
	setManualDiscountForTest(t, "kc-1", 30.0)

	server := priorityServerNoContributions()
	defer server.Close()
	client := newPriorityTestClient(server.URL)

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}

	base := GetCountryBasePrice("IL")
	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.NoError(t, err)

	donations, ok := findDiscount(eval.Discounts, DiscountTypeDonations)
	require.True(t, ok)
	assert.False(t, donations.Eligible)
	assert.False(t, donations.Applied)

	manual, ok := findDiscount(eval.Discounts, DiscountTypeManual)
	require.True(t, ok)
	assert.True(t, manual.Eligible)
	assert.True(t, manual.Applied)
	assert.Equal(t, 30.0, manual.AmountPct)

	assert.InDelta(t, base.Amount*0.70, eval.FinalPrice.Amount, 1e-9)
}

func TestEvaluateV2Price_ManualBeatsDonations(t *testing.T) {
	// Manual 70% > donations 55%. Donations still appears as Eligible=true, Applied=false.
	setManualDiscountForTest(t, "kc-1", 70.0)

	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100)
	defer server.Close()
	client := newPriorityTestClient(server.URL)

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.NoError(t, err)

	donations, _ := findDiscount(eval.Discounts, DiscountTypeDonations)
	assert.True(t, donations.Eligible)
	assert.False(t, donations.Applied)

	manual, _ := findDiscount(eval.Discounts, DiscountTypeManual)
	assert.True(t, manual.Eligible)
	assert.True(t, manual.Applied)

	assert.InDelta(t, base.Amount*0.30, eval.FinalPrice.Amount, 1e-9)
}

func TestEvaluateV2Price_DonationsBeatsManual(t *testing.T) {
	// Manual 30% < donations 55% → donations wins.
	setManualDiscountForTest(t, "kc-1", 30.0)

	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100)
	defer server.Close()
	client := newPriorityTestClient(server.URL)

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.NoError(t, err)

	donations, _ := findDiscount(eval.Discounts, DiscountTypeDonations)
	assert.True(t, donations.Eligible)
	assert.True(t, donations.Applied)

	manual, _ := findDiscount(eval.Discounts, DiscountTypeManual)
	assert.True(t, manual.Eligible)
	assert.False(t, manual.Applied)

	assert.InDelta(t, base.Amount*(1-DonationsDiscountAmtPct/100), eval.FinalPrice.Amount, 1e-9)
}

func TestEvaluateV2Price_TieGoesToDonations(t *testing.T) {
	// Manual at exactly 55% — ties go to donations (first in slice, strict > in winner loop).
	setManualDiscountForTest(t, "kc-1", 55.0)

	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100)
	defer server.Close()
	client := newPriorityTestClient(server.URL)

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", email, "IL")
	require.NoError(t, err)

	donations, _ := findDiscount(eval.Discounts, DiscountTypeDonations)
	manual, _ := findDiscount(eval.Discounts, DiscountTypeManual)
	assert.True(t, donations.Applied, "donations wins ties (first in slice)")
	assert.False(t, manual.Applied)
}

func TestEvaluateV2Price_ManualEntryOmittedWhenNotMatched(t *testing.T) {
	// No manual entry for this keycloak — slice should contain only donations.
	server := priorityServerNoContributions()
	defer server.Close()
	client := newPriorityTestClient(server.URL)

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-unmatched": {PrimaryEmail: &email}},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-unmatched", email, "IL")
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 1)
	assert.Equal(t, DiscountTypeDonations, eval.Discounts[0].Type)
}

func TestEvaluateV2Price_SpouseInMap_PrimaryNot_DonationsNotEligible(t *testing.T) {
	// Only the spouse has a manual entry. Primary's evaluation must NOT carry it.
	// Spouse's own billing run will apply their manual entry in their own evaluation.
	setManualDiscountForTest(t, "kc-spouse", 60.0)

	server := priorityServerNoContributions()
	defer server.Close()
	client := newPriorityTestClient(server.URL)

	primaryEmail := "primary@x.com"
	spouseEmail := "spouse@x.com"
	spouseKC := "kc-spouse"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1":   {PrimaryEmail: &primaryEmail, SpouseKeycloakID: &spouseKC},
			spouseKC: {PrimaryEmail: &spouseEmail},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, 10, "kc-1", primaryEmail, "IL")
	require.NoError(t, err)

	_, hasManual := findDiscount(eval.Discounts, DiscountTypeManual)
	assert.False(t, hasManual, "primary's evaluation must not leak spouse's manual entry")
	assert.Equal(t, eval.CountryBase.Amount, eval.FinalPrice.Amount)
}

func TestV2PricingEvaluation_Public_PreservesApplied(t *testing.T) {
	base := nisBase(180)
	props := baseProps(1)
	eval := makeEval(base, 101, Price{Amount: 81, Currency: common.CurrencyNIS}, props, true)
	eval.Discounts[0].Applied = true
	eval.Discounts = append(eval.Discounts, Discount{
		Type: DiscountTypeManual, AmountPct: 30.0, Eligible: true, Applied: false,
	})

	pub := eval.Public()
	require.Len(t, pub.Discounts, 2)
	assert.True(t, pub.Discounts[0].Applied)
	assert.False(t, pub.Discounts[1].Applied)
	// Properties still stripped.
	assert.Nil(t, pub.Discounts[0].Properties)
}

func TestBuildExplain_IncludesStep4bWhenManualMatched(t *testing.T) {
	base := nisBase(180)
	props := baseProps(1)
	eval := makeEval(base, 101, Price{Amount: 180 * 0.70, Currency: common.CurrencyNIS}, props, false)
	eval.Discounts = append(eval.Discounts, Discount{
		Type: DiscountTypeManual, AmountPct: 30.0, Eligible: true, Applied: true,
	})

	lines := buildExplain(eval)
	require.Len(t, lines, 6)
	assert.Contains(t, lines[4], "4b. manual discount")
	assert.Contains(t, lines[4], "30% off")
	assert.Contains(t, lines[5], "#101")
	assert.Contains(t, lines[5], "126.00 NIS") // 180 × 0.70
}

func TestBuildExplain_OmitsStep4bWhenNoManualEntry(t *testing.T) {
	base := nisBase(180)
	props := baseProps(1)
	eval := makeEval(base, 101, Price{Amount: 180, Currency: common.CurrencyNIS}, props, false)

	lines := buildExplain(eval)
	assert.Len(t, lines, 5)
	for _, line := range lines {
		assert.NotContains(t, line, "manual discount")
	}
}
