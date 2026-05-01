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

func TestBuildDonationsDiscount_WithSpouse_BelowDoubleAnnual_NeitherGets(t *testing.T) {
	// annual = 180*12 = 2160, 2×annual = 4320; combined = 3000 < 4320
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 3000}
	d, primaryGets, _ := buildDonationsDiscount(sums, base, 180, 1, 1, "spouse-kc")
	assert.False(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_WithSpouse_AboveDoubleAnnual_BothGet(t *testing.T) {
	// combined = 4400 >= 4320
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 4400}
	d, primaryGets, _ := buildDonationsDiscount(sums, base, 180, 1, 1, "spouse-kc")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.True(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_BelowAnnual_NoDiscount(t *testing.T) {
	// annual = 2160; combined = 1200 < 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 1200}
	d, primaryGets, _ := buildDonationsDiscount(sums, base, 180, 1, 0, "")
	assert.False(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_AboveAnnual_PrimaryGets(t *testing.T) {
	// combined = 2200 >= 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2200}
	d, primaryGets, _ := buildDonationsDiscount(sums, base, 180, 1, 0, "")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
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
	d, _, _ := buildDonationsDiscount(sums, base, 180, 2, 1, "spouse-kc-id")
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

// --- buildDonationsDiscount explain lines ---

func TestBuildDonationsDiscount_ExplainLines_NoSpouseNoDiscount(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 100}
	_, _, lines := buildDonationsDiscount(sums, base, 180, 2, 0, "")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "primary(2)")
	assert.Contains(t, lines[0], "ok")
	assert.Equal(t, "donations: aggregate per currency → convert to NIS", lines[1])
	assert.Contains(t, lines[2], "< 2160.00 NIS/yr")
	assert.Contains(t, lines[2], "no discount")
	assert.NotContains(t, lines[2], "→ NIS") // NIS base: no conversion marker
}

func TestBuildDonationsDiscount_ExplainLines_NoSpouseWithDiscount(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2200}
	_, _, lines := buildDonationsDiscount(sums, base, 180, 2, 0, "")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[2], ">= 2160.00 NIS/yr")
	assert.Contains(t, lines[2], "55% off")
}

func TestBuildDonationsDiscount_ExplainLines_WithSpouseBothGet(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 4400}
	_, _, lines := buildDonationsDiscount(sums, base, 180, 2, 1, "spouse-kc")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "primary(2) + spouse(1) = 3 unique")
	assert.Contains(t, lines[2], ">= 4320.00 NIS/yr")
	assert.Contains(t, lines[2], "both get 55% off")
}

func TestBuildDonationsDiscount_ExplainLines_WithSpouseNeitherGets(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2000}
	_, _, lines := buildDonationsDiscount(sums, base, 180, 2, 1, "spouse-kc")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[2], "< 4320.00 NIS/yr")
	assert.Contains(t, lines[2], "no discount")
}

func TestBuildDonationsDiscount_ExplainLines_ForeignCurrencyHasNISMarker(t *testing.T) {
	base := CountryBasePrice{Price: Price{Amount: 10, Currency: common.CurrencyUSD}, Group: "Medium"}
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 0}
	_, _, lines := buildDonationsDiscount(sums, base, 31, 1, 0, "")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[2], "120.00 USD/yr (→ NIS)")
	assert.NotContains(t, lines[2], "NIS/yr") // amount is in USD, not NIS
}

func TestBuildDonationsDiscount_ExplainLines_FetchNoteIncluded(t *testing.T) {
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 0, fetchNote: "no Priority account for emails: x@x.com"}
	_, _, lines := buildDonationsDiscount(sums, base, 180, 1, 0, "")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "ok: no Priority account")
}

// --- fetchDonationSums ---

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
