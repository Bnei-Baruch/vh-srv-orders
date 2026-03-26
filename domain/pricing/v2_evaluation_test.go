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

func TestBuildDonationsDiscount_BelowHalf_NoDiscount(t *testing.T) {
	// annual = 180*12 = 2160, half = 1080; combined = 500 < 1080
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 500, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 20, 1, 1, "spouse-kc")
	assert.False(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_AboveHalf_PrimarySmaller_PrimaryGets(t *testing.T) {
	// combined = 1200, half = 1080 <= 1200 < 2160; primaryID=10 < spouseID=20
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 1200, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 20, 1, 1, "spouse-kc")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_AboveHalf_SpouseSmaller_SpouseGets(t *testing.T) {
	// combined = 1200; spouseID=5 < primaryID=10
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 1200, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 5, 1, 1, "spouse-kc")
	assert.True(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.True(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_AboveAnnual_BothGet(t *testing.T) {
	// combined = 2200 >= 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2200, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 20, 1, 1, "spouse-kc")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.True(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_BelowHalf(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 500, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 0, 1, 0, "")
	assert.False(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
	assert.Zero(t, props.SpouseAccountID)
}

func TestBuildDonationsDiscount_NoSpouse_AboveHalf_PrimaryGets(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 1200, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 0, 1, 0, "")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_AboveAnnual_PrimaryGets(t *testing.T) {
	// No spouse — combined >= annual still means only primary gets it
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2200, fetched: true}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 10, 0, 1, 0, "")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_PropertiesStoredCorrectly(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{
		perCurrency:   map[string]float64{},
		totalNIS:      1200,
		fetched:       true,
		successEmails: []string{"a@x.com"},
		fetchNote:     "some note",
	}
	d, _ := buildDonationsDiscount(sums, base, 180, 10, 20, 2, 1, "spouse-kc-id")
	props := unmarshalDonationsProps(t, d)
	assert.Equal(t, 2, props.PrimaryEmailCount)
	assert.Equal(t, 1, props.SpouseEmailCount)
	assert.Equal(t, 20, props.SpouseAccountID)
	assert.Equal(t, "spouse-kc-id", props.SpouseKeycloakID)
	assert.True(t, props.DonationsFetched)
	assert.Equal(t, []string{"a@x.com"}, props.DonationsFetchedEmails)
	assert.Equal(t, "some note", props.DonationsFetchNote)
	assert.Equal(t, Price{Amount: 180 * 12, Currency: common.CurrencyNIS}, props.AnnualBase)
	assert.Equal(t, DiscountTypeDonations, d.Type)
	assert.Equal(t, 50.0, d.AmountPct)
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
	result := fetchDonationSums(context.Background(), client, []string{"unknown@x.com"}, 3.1, 3.6)

	assert.True(t, result.fetched)
	assert.Contains(t, result.fetchNote, "unknown@x.com") // recorded as "no Priority account"
	assert.Empty(t, result.fetchError)                    // not a real error
	assert.Equal(t, 0.0, result.totalNIS)
}

func TestFetchDonationSums_APIError_RecordedInNote(t *testing.T) {
	// One email errors (non-customer-not-found), one succeeds with contributions
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
				Value: []priority.Customer{{CustName: "CUST001", Email: "good@x.com"}},
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
	result := fetchDonationSums(context.Background(), client, []string{"bad@x.com", "good@x.com"}, 3.1, 3.6)

	assert.True(t, result.fetched) // partial failure → still fetched=true
	assert.Contains(t, result.fetchError, "bad@x.com")
	assert.Empty(t, result.fetchNote) // no "customer not found" emails
	assert.Equal(t, 100.0, result.totalNIS)
}

func TestFetchDonationSums_AllEmailsFail_FetchedFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	result := fetchDonationSums(context.Background(), client, []string{"a@x.com", "b@x.com"}, 3.1, 3.6)

	assert.False(t, result.fetched)
	assert.NotEmpty(t, result.fetchError)
	assert.Empty(t, result.fetchNote)
	assert.Equal(t, 0.0, result.totalNIS)
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
	result := fetchDonationSums(context.Background(), client, []string{"usd@x.com", "nis@x.com"}, 3.1, 3.6)

	assert.True(t, result.fetched)
	assert.Empty(t, result.fetchNote)
	assert.InDelta(t, 510.0, result.totalNIS, 0.001)
}

// --- buildExplain ---

// baseProps returns a minimal DonationsDiscountProperties for formula tests.
func baseProps(primaryEmailCount int, fetched bool) DonationsDiscountProperties {
	return DonationsDiscountProperties{
		PrimaryEmailCount: primaryEmailCount,
		DonationsFetched:  fetched,
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
	props := baseProps(2, true)
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
	assert.Contains(t, lines[3], "no discount")
	assert.Contains(t, lines[3], "1080.00 NIS/yr")
	assert.NotContains(t, lines[3], "→ NIS")
	assert.Contains(t, lines[4], "#101")
	assert.Contains(t, lines[4], "180.00 NIS")
}

func TestBuildFormula_NoSpousePrimaryDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2, true)
	eval := makeEval(base, 101, Price{Amount: 90, Currency: common.CurrencyNIS}, props, true)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[3], "primary gets 50% off")
	assert.Contains(t, lines[4], "90.00 NIS")
}

func TestBuildFormula_WithSpouseBothDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2, true)
	props.SpouseAccountID = 202
	props.SpouseEmailCount = 1
	props.SpouseGetsDiscount = true
	eval := makeEval(base, 101, Price{Amount: 90, Currency: common.CurrencyNIS}, props, true)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[1], "primary(2) + spouse(1) = 3 unique")
	assert.Contains(t, lines[3], "both members get 50% off")
	assert.Contains(t, lines[4], "#101")
	assert.Contains(t, lines[4], "#202")
}

func TestBuildFormula_WithSpousePrimaryDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2, true)
	props.SpouseAccountID = 202 // spouse > primary → primary gets discount
	props.SpouseEmailCount = 1
	props.SpouseGetsDiscount = false
	eval := makeEval(base, 101, Price{Amount: 90, Currency: common.CurrencyNIS}, props, true)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[3], "primary gets 50% off")
	assert.Contains(t, lines[3], "primary_id[101] <= spouse_id[202]")
}

func TestBuildFormula_WithSpouseSpouseDiscount(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2, true)
	props.SpouseAccountID = 50 // spouse < primary → spouse gets discount
	props.SpouseEmailCount = 1
	props.SpouseGetsDiscount = true
	eval := makeEval(base, 101, Price{Amount: 180, Currency: common.CurrencyNIS}, props, true)

	lines := buildExplain(eval)
	require.Len(t, lines, 5)
	assert.Contains(t, lines[3], "spouse gets 50% off")
	assert.Contains(t, lines[3], "spouse_id[50] < primary_id[101]")
}

func TestBuildFormula_ForeignCurrencyOriginalCurrency(t *testing.T) {
	base := CountryBasePrice{Price: Price{Amount: 10, Currency: common.CurrencyUSD}, Group: "Medium"}
	props := DonationsDiscountProperties{
		PrimaryEmailCount: 1,
		DonationsFetched:  true,
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
	// Step 4: thresholds in original currency with → NIS marker
	assert.Contains(t, lines[3], "60.00 USD/yr (→ NIS)")
	assert.NotContains(t, lines[3], "372")
}

// --- Public ---

func TestV2PricingEvaluation_Public_StripsExplainAndProperties(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2, true)
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
	props := baseProps(2, true)
	eval := makeEval(base, 101, Price{Amount: 90, Currency: common.CurrencyNIS}, props, true)
	eval.Explain = []string{"step 1"}

	_ = eval.Public()

	assert.NotNil(t, eval.Explain)
	assert.NotNil(t, eval.Discounts[0].Properties)
}

func TestV2PricingEvaluation_Public_PreservesNonSensitiveFields(t *testing.T) {
	base := nisBase(180)
	props := baseProps(2, false)
	eval := makeEval(base, 101, Price{Amount: 180, Currency: common.CurrencyNIS}, props, false)

	pub := eval.Public()

	assert.Equal(t, eval.AccountID, pub.AccountID)
	assert.Equal(t, eval.CountryCode, pub.CountryCode)
	assert.Equal(t, eval.CountryBase, pub.CountryBase)
	assert.Equal(t, eval.FinalPrice, pub.FinalPrice)
}
