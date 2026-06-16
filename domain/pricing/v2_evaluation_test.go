package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	accountingmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// notFoundAccountingClient returns a mock that responds Found:false for any email
// on both the QuickBooks and Europe endpoints, effectively isolating the existing
// tests to Priority-only behavior.
func notFoundAccountingClient(t *testing.T) accounting.AccountingService {
	m := accountingmocks.NewMockAccountingService(t)
	m.EXPECT().GetLastContributions(mock.Anything, mock.Anything, mock.Anything).
		Return(&accounting.ContributionsResult{Found: false}, nil).Maybe()
	m.EXPECT().GetEuropeContributions(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, emails []string) (*accounting.EuropeContributionsResult, error) {
			return europeNotFoundResult(emails), nil
		}).Maybe()
	return m
}

// europeNotFoundResult builds a Europe batch result marking every email not found.
func europeNotFoundResult(emails []string) *accounting.EuropeContributionsResult {
	res := &accounting.EuropeContributionsResult{LookbackMonths: 12}
	for _, email := range emails {
		res.Results = append(res.Results, accounting.EuropeContributionEntry{
			IdentifierType: "email", Identifier: email, Found: false,
		})
	}
	return res
}

const testQuickbooksCompanyID = "test-co"

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
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 1, 1, "spouse-kc")
	assert.False(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_WithSpouse_AboveDoubleAnnual_BothGet(t *testing.T) {
	// combined = 4400 >= 4320
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 4400}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 1, 1, "spouse-kc")
	assert.True(t, d.Eligible)
	assert.True(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.True(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_BelowAnnual_NoDiscount(t *testing.T) {
	// annual = 2160; combined = 1200 < 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 1200}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 1, 0, "")
	assert.False(t, d.Eligible)
	assert.False(t, primaryGets)
	props := unmarshalDonationsProps(t, d)
	assert.False(t, props.SpouseGetsDiscount)
}

func TestBuildDonationsDiscount_NoSpouse_AboveAnnual_PrimaryGets(t *testing.T) {
	// combined = 2200 >= 2160
	base := nisBase(180)
	sums := donationSums{perCurrency: map[string]float64{}, totalNIS: 2200}
	d, primaryGets := buildDonationsDiscount(sums, base, 180, 1, 0, "")
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
	d, _ := buildDonationsDiscount(sums, base, 180, 2, 1, "spouse-kc-id")
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
	result, err := fetchDonationSums(context.Background(), client, notFoundAccountingClient(t), testQuickbooksCompanyID, []string{"unknown@x.com"}, 3.1, 3.6)

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
	_, err := fetchDonationSums(context.Background(), client, notFoundAccountingClient(t), testQuickbooksCompanyID, []string{"bad@x.com"}, 3.1, 3.6)

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
	_, err := fetchDonationSums(context.Background(), client, notFoundAccountingClient(t), testQuickbooksCompanyID, []string{"good@x.com", "bad@x.com"}, 3.1, 3.6)

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
	result, err := fetchDonationSums(context.Background(), client, notFoundAccountingClient(t), testQuickbooksCompanyID, []string{"usd@x.com", "nis@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	// Priority found both emails; accounting mock returns Found:false for both → note records the QB miss.
	assert.NotContains(t, result.fetchNote, "Priority")
	assert.Contains(t, result.fetchNote, "no QuickBooks record")
	assert.InDelta(t, 510.0, result.totalNIS, 0.001)
}

// noPriorityCustomersServer returns a Priority test server where no customers exist for any email.
func noPriorityCustomersServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(priority.CustomerODataResponse{Value: []priority.Customer{}})
	}))
}

func TestFetchDonationSums_AccountingOnly_AggregatesContributions(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "qb@x.com", mock.Anything).Return(&accounting.ContributionsResult{
		Found: true,
		Total: map[string]float64{common.CurrencyUSD: 100},
	}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"qb@x.com"}).
		Return(europeNotFoundResult([]string{"qb@x.com"}), nil).Once()

	result, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"qb@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.InDelta(t, 310.0, result.totalNIS, 0.001) // 100 USD * 3.1 = 310 NIS
	assert.Equal(t, []string{"qb@x.com"}, result.successEmails)
	assert.Contains(t, result.fetchNote, "no Priority record")
	assert.NotContains(t, result.fetchNote, "no QuickBooks record")
	assert.Contains(t, result.fetchNote, "no Europe record")
}

func TestFetchDonationSums_AllSources_SumByCurrency(t *testing.T) {
	// Priority returns 200 NIS; QuickBooks returns 100 USD; Europe returns 100 EUR
	// → total = 200 + 310 + 360 = 870 NIS.
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "CUSTOMERS") && r.URL.Query().Get("$filter") != "" {
			json.NewEncoder(w).Encode(priority.CustomerODataResponse{Value: []priority.Customer{{CustName: "C1"}}})
		} else {
			json.NewEncoder(w).Encode(priority.AccountReceivableODataResponse{Value: []priority.AccountReceivableItem{
				{ACCNAME: "40001", DEBIT: 200, CODE: common.CurrencyNIS, FNCDATE: validDate},
			}})
		}
	}))
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "user@x.com", mock.Anything).Return(&accounting.ContributionsResult{
		Found: true,
		Total: map[string]float64{common.CurrencyUSD: 100},
	}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"user@x.com"}).Return(&accounting.EuropeContributionsResult{
		LookbackMonths: 12,
		Results: []accounting.EuropeContributionEntry{{
			IdentifierType: "email", Identifier: "user@x.com", Found: true,
			Contributions: map[string]float64{common.CurrencyEUR: 100},
		}},
	}, nil).Once()

	result, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"user@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.InDelta(t, 870.0, result.totalNIS, 0.001)
	assert.Equal(t, []string{"user@x.com"}, result.successEmails)
	assert.Empty(t, result.fetchNote)
}

func TestFetchDonationSums_AccountingError_ReturnsErrDonationFetch(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "user@x.com", mock.Anything).
		Return(nil, fmt.Errorf("accounting service unreachable")).Once()

	_, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"user@x.com"}, 3.1, 3.6)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
	assert.Contains(t, err.Error(), "user@x.com")
}

func TestFetchDonationSums_AccountingFoundEmptyContributions_StillCountsAsSuccess(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	// Found:true but Total is empty — user exists in QB but has no qualifying contributions.
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "user@x.com", mock.Anything).
		Return(&accounting.ContributionsResult{Found: true, Total: map[string]float64{}}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"user@x.com"}).
		Return(europeNotFoundResult([]string{"user@x.com"}), nil).Once()

	result, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"user@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.Equal(t, 0.0, result.totalNIS)
	assert.Equal(t, []string{"user@x.com"}, result.successEmails)
	assert.Contains(t, result.fetchNote, "no Priority record")
	assert.NotContains(t, result.fetchNote, "no QuickBooks record")
}

func TestFetchDonationSums_PassesConfiguredCompanyIDToAccountingClient(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	expectedCompany := "qb-realm-123"
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(
		mock.Anything,
		"user@x.com",
		mock.MatchedBy(func(p *string) bool { return p != nil && *p == expectedCompany }),
	).Return(&accounting.ContributionsResult{Found: false}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"user@x.com"}).
		Return(europeNotFoundResult([]string{"user@x.com"}), nil).Once()

	_, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, expectedCompany, []string{"user@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
}

// --- addEuropeContributions ---

func TestAddEuropeContributions_FoundAccumulates(t *testing.T) {
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"a@x.com", "b@x.com"}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{
			{Identifier: "a@x.com", Found: true, Contributions: map[string]float64{common.CurrencyEUR: 100, common.CurrencyUSD: 50}},
			{Identifier: "b@x.com", Found: true, Contributions: map[string]float64{common.CurrencyEUR: 25}},
		},
	}, nil).Once()

	perCurrency := map[string]float64{}
	successSet := map[string]struct{}{}
	notFound, err := addEuropeContributions(context.Background(), mockAcc, []string{"a@x.com", "b@x.com"}, perCurrency, successSet)

	require.NoError(t, err)
	assert.Empty(t, notFound)
	assert.Equal(t, map[string]float64{common.CurrencyEUR: 125, common.CurrencyUSD: 50}, perCurrency)
	assert.Contains(t, successSet, "a@x.com")
	assert.Contains(t, successSet, "b@x.com")
}

func TestAddEuropeContributions_NotFoundListed(t *testing.T) {
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"a@x.com", "b@x.com"}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{
			{Identifier: "a@x.com", Found: true, Contributions: map[string]float64{common.CurrencyEUR: 100}},
			{Identifier: "b@x.com", Found: false},
		},
	}, nil).Once()

	perCurrency := map[string]float64{}
	successSet := map[string]struct{}{}
	notFound, err := addEuropeContributions(context.Background(), mockAcc, []string{"a@x.com", "b@x.com"}, perCurrency, successSet)

	require.NoError(t, err)
	assert.Equal(t, []string{"b@x.com"}, notFound)
	assert.NotContains(t, successSet, "b@x.com")
}

func TestAddEuropeContributions_MissingEntryTreatedAsNotFound(t *testing.T) {
	// Upstream omits an email from the results — defensive: treat as not found.
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"a@x.com", "missing@x.com"}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{
			{Identifier: "a@x.com", Found: true, Contributions: map[string]float64{common.CurrencyEUR: 100}},
		},
	}, nil).Once()

	perCurrency := map[string]float64{}
	successSet := map[string]struct{}{}
	notFound, err := addEuropeContributions(context.Background(), mockAcc, []string{"a@x.com", "missing@x.com"}, perCurrency, successSet)

	require.NoError(t, err)
	assert.Equal(t, []string{"missing@x.com"}, notFound)
}

func TestAddEuropeContributions_CaseInsensitiveIdentifierMatch(t *testing.T) {
	// Response identifier casing differs from the request email — must still match,
	// and successSet keeps the original-cased request email.
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"User@X.com"}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{
			{Identifier: "user@x.com", Found: true, Contributions: map[string]float64{common.CurrencyEUR: 100}},
		},
	}, nil).Once()

	perCurrency := map[string]float64{}
	successSet := map[string]struct{}{}
	notFound, err := addEuropeContributions(context.Background(), mockAcc, []string{"User@X.com"}, perCurrency, successSet)

	require.NoError(t, err)
	assert.Empty(t, notFound)
	assert.Contains(t, successSet, "User@X.com")
	assert.Equal(t, 100.0, perCurrency[common.CurrencyEUR])
}

func TestAddEuropeContributions_ErrorWrapsErrDonationFetch(t *testing.T) {
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("europe upstream unreachable")).Once()

	_, err := addEuropeContributions(context.Background(), mockAcc, []string{"a@x.com"}, map[string]float64{}, map[string]struct{}{})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
}

func TestAddEuropeContributions_EmptyEmailsNoCall(t *testing.T) {
	// No expectation set — any call to the mock would fail the test.
	mockAcc := accountingmocks.NewMockAccountingService(t)

	notFound, err := addEuropeContributions(context.Background(), mockAcc, nil, map[string]float64{}, map[string]struct{}{})

	require.NoError(t, err)
	assert.Empty(t, notFound)
}

func TestFetchDonationSums_EuropeNegativeEUR_ReducesTotal(t *testing.T) {
	// QuickBooks +100 USD, Europe -50 EUR (refunds exceed donations)
	// → 310 - 180 = 130 NIS.
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "user@x.com", mock.Anything).Return(&accounting.ContributionsResult{
		Found: true,
		Total: map[string]float64{common.CurrencyUSD: 100},
	}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"user@x.com"}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{{
			Identifier: "user@x.com", Found: true,
			Contributions: map[string]float64{common.CurrencyEUR: -50},
		}},
	}, nil).Once()

	result, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"user@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.InDelta(t, 130.0, result.totalNIS, 0.001)
}

func TestFetchDonationSums_EuropeOnly_AggregatesContributions(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "eu@x.com", mock.Anything).
		Return(&accounting.ContributionsResult{Found: false}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"eu@x.com"}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{{
			Identifier: "eu@x.com", Found: true,
			Contributions: map[string]float64{common.CurrencyEUR: 100},
		}},
	}, nil).Once()

	result, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"eu@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.InDelta(t, 360.0, result.totalNIS, 0.001) // 100 EUR * 3.6
	assert.Equal(t, []string{"eu@x.com"}, result.successEmails)
	assert.Contains(t, result.fetchNote, "no Priority record")
	assert.Contains(t, result.fetchNote, "no QuickBooks record")
	assert.NotContains(t, result.fetchNote, "no Europe record")
}

func TestFetchDonationSums_NoteIncludesAllThreeSources(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	result, err := fetchDonationSums(context.Background(), priorityClient, notFoundAccountingClient(t), testQuickbooksCompanyID, []string{"miss@x.com"}, 3.1, 3.6)

	require.NoError(t, err)
	assert.Equal(t, 0.0, result.totalNIS)
	assert.Empty(t, result.successEmails)
	assert.Equal(t,
		"no Priority record for: miss@x.com; no QuickBooks record for: miss@x.com; no Europe record for: miss@x.com",
		result.fetchNote)
}

func TestFetchDonationSums_EuropeError_ReturnsErrDonationFetch(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	priorityClient := newPriorityTestClient(server.URL)

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, "user@x.com", mock.Anything).
		Return(&accounting.ContributionsResult{Found: false}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{"user@x.com"}).
		Return(nil, fmt.Errorf("europe upstream unreachable")).Once()

	_, err := fetchDonationSums(context.Background(), priorityClient, mockAcc, testQuickbooksCompanyID, []string{"user@x.com"}, 3.1, 3.6)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
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
	assert.Contains(t, lines[1], "fetch donations from all sources (Priority, QuickBooks, Europe; last 12mo)")
	assert.Contains(t, lines[1], "ok")
	assert.Equal(t, "3. sum all donations per currency → convert each to NIS", lines[2])
	assert.Contains(t, lines[3], "no discount")
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
	assert.Contains(t, lines[3], "primary gets 55% off")
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
	assert.Contains(t, lines[3], "both members get 55% off")
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
	assert.Contains(t, lines[3], "no discount")
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil)
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil)
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", "fallback@x.com", "IL", nil)
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", primaryEmail, "IL", nil)
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", primaryEmail, "IL", nil)
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "AU", nil)
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", primaryEmail, "IL", nil)
	require.NoError(t, err)
	assert.NotNil(t, eval)
}

func TestEvaluateV2Price_ProfileServiceError_ReturnsError(t *testing.T) {
	server := priorityServerNoContributions()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	profileSvc := &errorProfileService{err: fmt.Errorf("connection refused")}

	_, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", "test@x.com", "IL", nil)
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

	_, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", primaryEmail, "IL", nil)
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

func TestEvaluateV2Price_DonationFetchError_ReturnsDegradedDiscount(t *testing.T) {
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil)
	require.NoError(t, err)
	require.Len(t, eval.Discounts, 1)
	assert.Equal(t, DiscountTypeDonations, eval.Discounts[0].Type)
	assert.True(t, eval.Discounts[0].Error)
	assert.False(t, eval.Discounts[0].Eligible)
	assert.True(t, eval.HasDiscountErrors())
	base := GetCountryBasePrice("IL")
	assert.Equal(t, base.Amount, eval.FinalPrice.Amount)
}

func TestEvaluateV2Price_PartialDonationFetchError_ReturnsDegradedDiscount(t *testing.T) {
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

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", good, "IL", nil)
	require.NoError(t, err)
	require.Len(t, eval.Discounts, 1)
	assert.True(t, eval.Discounts[0].Error)
	assert.False(t, eval.Discounts[0].Eligible)
	assert.True(t, eval.HasDiscountErrors())
}

func TestEvaluateV2Price_EuropeDonationsAloneGrantDiscount(t *testing.T) {
	// EU country (Germany, EUR base). Priority and QuickBooks have no record;
	// Europe donations alone cross the annual threshold → 55% off.
	base := GetCountryBasePrice("DE")
	server := noPriorityCustomersServer()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	email := "primary@x.de"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &email},
		},
	}

	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, email, mock.Anything).
		Return(&accounting.ContributionsResult{Found: false}, nil).Once()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, []string{email}).Return(&accounting.EuropeContributionsResult{
		Results: []accounting.EuropeContributionEntry{{
			Identifier: email, Found: true,
			Contributions: map[string]float64{common.CurrencyEUR: base.Amount*12 + 100},
		}},
	}, nil).Once()

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, mockAcc, testQuickbooksCompanyID, 10, "kc-1", email, "DE", nil)
	require.NoError(t, err)

	assert.Equal(t, common.CurrencyEUR, eval.CountryBase.Currency)
	assert.True(t, eval.Discounts[0].Eligible)
	assert.Equal(t, base.Amount*(1-DonationsDiscountAmtPct/100), eval.FinalPrice.Amount)
	props := unmarshalDonationsProps(t, eval.Discounts[0])
	assert.Equal(t, []string{email}, props.DonationsFetchedEmails)
}

func TestEvaluateV2Price_EuropeCountry_NoDonations_FullPrice(t *testing.T) {
	base := GetCountryBasePrice("DE")
	server := noPriorityCustomersServer()
	defer server.Close()

	client := newPriorityTestClient(server.URL)
	email := "primary@x.de"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{
			"kc-1": {PrimaryEmail: &email},
		},
	}

	eval, err := EvaluateV2Price(context.Background(), profileSvc, client, notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "DE", nil)
	require.NoError(t, err)

	assert.False(t, eval.Discounts[0].Eligible)
	assert.Equal(t, Price{Amount: base.Amount, Currency: common.CurrencyEUR}, eval.FinalPrice)
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

// --- ManualDiscountProvider integration ---

func TestEvaluateV2Price_WithDiscountProvider_Applied(t *testing.T) {
	// No donations → final price = base (180 NIS). Manual 50% off = 90 NIS < 180 NIS → applied.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	pct := 50.0
	props, _ := json.Marshal(repo.ManualDiscountProperties{DiscountPct: &pct})
	provider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return &repo.ManualDiscount{ID: 1, Type: "percent", Properties: null.JSONFrom(props)}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeDonations, eval.Discounts[0].Type)
	assert.Equal(t, DiscountTypeManual, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 90.0, eval.FinalPrice.Amount)
	assert.False(t, eval.HasDiscountErrors())
}

func TestEvaluateV2Price_WithDiscountProvider_NoActiveDiscount(t *testing.T) {
	// Provider returns nil — manual entry is present but ineligible.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	provider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return nil, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeDonations, eval.Discounts[0].Type)
	assert.Equal(t, DiscountTypeManual, eval.Discounts[1].Type)
	assert.False(t, eval.Discounts[1].Eligible)
	assert.False(t, eval.Discounts[1].Error)
	assert.False(t, eval.HasDiscountErrors())
}

func TestEvaluateV2Price_WithDiscountProvider_DonationsWins(t *testing.T) {
	// Donations gives 55% off (base 80 NIS → 36 NIS).
	// Manual gives 30% off (80 * 0.70 = 56 NIS) — more expensive than donations result.
	// Expected: donations FinalPrice (36 NIS), manual discount entry is ineligible.
	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100) // above single-person threshold
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	pct := 30.0
	props, _ := json.Marshal(repo.ManualDiscountProperties{DiscountPct: &pct})
	provider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return &repo.ManualDiscount{ID: 1, Type: "percent", Properties: null.JSONFrom(props)}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", provider)
	require.NoError(t, err)

	donationsPrice := base.Amount * (1 - DonationsDiscountAmtPct/100)
	assert.Equal(t, donationsPrice, eval.FinalPrice.Amount, "donations discount should win")
	require.Len(t, eval.Discounts, 2)
	assert.True(t, eval.Discounts[0].Eligible, "donations discount eligible")
	assert.False(t, eval.Discounts[1].Eligible, "manual discount not applied — would be more expensive")
}

func TestEvaluateV2Price_WithDiscountProvider_ManualWins(t *testing.T) {
	// Donations gives 55% off (base 80 NIS → 36 NIS).
	// Manual gives 60% off (80 * 0.40 = 32 NIS) — cheaper than donations result.
	// Expected: manual FinalPrice (32 NIS), manual discount entry is eligible.
	base := GetCountryBasePrice("IL")
	server := priorityServerWithContributions(base.Amount*12 + 100) // above single-person threshold
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	pct := 60.0
	props, _ := json.Marshal(repo.ManualDiscountProperties{DiscountPct: &pct})
	provider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return &repo.ManualDiscount{ID: 2, Type: "percent", Properties: null.JSONFrom(props)}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", provider)
	require.NoError(t, err)

	manualPrice := math.Round(base.Amount*(1-pct/100)*100) / 100
	assert.Equal(t, manualPrice, eval.FinalPrice.Amount, "manual discount should win")
	require.Len(t, eval.Discounts, 2)
	assert.True(t, eval.Discounts[0].Eligible, "donations discount eligible")
	assert.True(t, eval.Discounts[1].Eligible, "manual discount applied — cheaper than donations")
}

func TestEvaluateV2Price_WithDiscountProvider_FetchError(t *testing.T) {
	// Provider returns an error — manual entry has Error: true, HasDiscountErrors returns true.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	provider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return nil, fmt.Errorf("DB connection lost")
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeManual, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Error)
	assert.False(t, eval.Discounts[1].Eligible)
	assert.True(t, eval.HasDiscountErrors())
	require.NotEmpty(t, eval.Explain)
	assert.Contains(t, eval.Explain[len(eval.Explain)-1], "manual_discount")
	assert.Contains(t, eval.Explain[len(eval.Explain)-1], "fetch error")
}
