package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	accountingmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// newCheckoutTestApp wires the v2 pricing dependencies. With emptyPriority and
// the no-contributions accounting mocks, an IL account evaluates to the base
// v2 price: 180 NIS, no discounts.
func newCheckoutTestApp(t *testing.T, priorityHandler http.HandlerFunc) *App {
	priorityServer := httptest.NewServer(priorityHandler)
	t.Cleanup(priorityServer.Close)

	origPriorityURL := common.Config.PriorityBaseURL
	common.Config.PriorityBaseURL = priorityServer.URL
	t.Cleanup(func() { common.Config.PriorityBaseURL = origPriorityURL })

	a := NewTestApp(t)
	t.Cleanup(func() { CloseTestApp(a) })

	a.ordersAPI.SetPriorityClient(priority.NewClient())
	a.ordersAPI.SetProfileService(&notFoundProfileService{})
	mockAcc := accountingmocks.NewMockAccountingService(t)
	mockAcc.EXPECT().GetLastContributions(mock.Anything, mock.Anything, mock.Anything).
		Return(&accounting.ContributionsResult{Found: false}, nil).Maybe()
	mockAcc.EXPECT().GetEuropeContributions(mock.Anything, mock.Anything).
		Return(&accounting.EuropeContributionsResult{}, nil).Maybe()
	a.ordersAPI.SetAccountingService(mockAcc)
	a.ordersAPI.SetQuickbooksCompanyID("test-company")
	return a
}

func emptyPriority(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"value":[]}`))
}

func createCheckoutAccount(t *testing.T, a *App, country string) {
	POST_ROOT(t, a, "/v2/account/", repo.Account{
		UserKey: null.StringFrom(USER_KEY),
		Country: null.StringFrom(country),
		Email:   null.StringFrom("checkout@example.com"),
	}, http.StatusCreated)
}

func membershipCheckout(amount float64, currency string) repo.RequestOrder {
	return repo.RequestOrder{
		FirstName:   null.StringFrom("Test"),
		LastName:    null.StringFrom("User"),
		Email:       null.StringFrom("checkout@example.com"),
		UserKey:     null.StringFrom(USER_KEY),
		Amount:      null.Float64From(amount),
		Currency:    null.StringFrom(currency),
		Type:        null.StringFrom("recurring"),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		TerminalId:  null.StringFrom("ben_dummy_pelecard"),
		SuccessURL:  null.StringFrom("http://test/success"),
		ErrorURL:    null.StringFrom("http://test/error"),
		CancelURL:   null.StringFrom("http://test/cancel"),
	}
}

var paymentIDRe = regexp.MustCompile(`additional_details_param_x=m-(\d+)`)

func paymentFromDummyURL(t *testing.T, a *App, got gin.H) *repo.Payment {
	m := paymentIDRe.FindStringSubmatch(got["url"].(string))
	require.Len(t, m, 2, "payment id in dummy terminal url")
	id, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	p, err := a.repo.GetPaymentByID(context.Background(), id)
	require.NoError(t, err)
	return p
}

func requestBody(t *testing.T, request any) io.Reader {
	b, err := json.Marshal(request)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

func paymentsCount(t *testing.T, a *App) int {
	var n int
	err := a.repo.(*repo.OrdersDB).QueryRow(context.Background(), "SELECT count(*) FROM payments").Scan(&n)
	require.NoError(t, err)
	return n
}

func TestCheckout_V2_BelowPrice_Rejected(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	got := POST_ROOT(t, a, "/v2/transaction/", membershipCheckout(100, common.CurrencyNIS), http.StatusBadRequest)
	assert.Equal(t, "amount does not match the current price", got["error"])
	assert.Equal(t, 180.0, got["amount"])
	assert.Equal(t, common.CurrencyNIS, got["currency"])
	assert.Equal(t, 0, paymentsCount(t, a))
}

func TestCheckout_V2_AtPrice_ChargesAndPersistsEvaluation(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	got := POST_ROOT(t, a, "/v2/transaction/", membershipCheckout(180, common.CurrencyNIS), http.StatusOK)
	p := paymentFromDummyURL(t, a, got)
	assert.Equal(t, 180.0, p.Amount.Float64)
	assert.Equal(t, "v2", p.PricingVersion.String)

	var evaluation string
	err := a.repo.(*repo.OrdersDB).QueryRow(context.Background(),
		"SELECT pricing_evaluation::text FROM payments WHERE id = $1", p.ID).Scan(&evaluation)
	require.NoError(t, err)
	assert.Contains(t, evaluation, `"discounts"`)
}

func TestCheckout_V2_AbovePrice_Rejected(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	got := POST_ROOT(t, a, "/v2/transaction/", membershipCheckout(250, common.CurrencyNIS), http.StatusBadRequest)
	assert.Equal(t, "amount does not match the current price", got["error"])
	assert.Equal(t, 0, paymentsCount(t, a))
}

// Fractional amounts can't be exercised through the endpoint (orders.Amount is
// an integer column), so the float tolerance is tested on the comparison itself.
// Noise realistically comes from discount math on the resolved price.
func TestSameAmount_FloatTolerance(t *testing.T) {
	assert.True(t, sameAmount(180, 180))
	assert.True(t, sameAmount(180, 179.999999999999)) // float noise doesn't block
	assert.True(t, sameAmount(180, 180.000000000001))
	assert.True(t, sameAmount(90, 89.99999999999999)) // e.g. noisy 50% discount math
	assert.False(t, sameAmount(180, 180.01)) // a real cent difference blocks
	assert.False(t, sameAmount(180, 179.99))
	assert.False(t, sameAmount(180, 181))
}

func TestCheckout_V2_CurrencyMismatch_Rejected(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	got := POST_ROOT(t, a, "/v2/transaction/", membershipCheckout(180, common.CurrencyUSD), http.StatusBadRequest)
	assert.Equal(t, "currency does not match the current price", got["error"])
	assert.Equal(t, common.CurrencyNIS, got["currency"])
	assert.Equal(t, 0, paymentsCount(t, a))
}

func TestCheckout_NonMembership_Bypassed(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	req := membershipCheckout(1, common.CurrencyUSD)
	req.ProductType = null.StringFrom("ticket")
	got := POST_ROOT(t, a, "/v2/transaction/", req, http.StatusOK)
	p := paymentFromDummyURL(t, a, got)
	assert.Equal(t, 1.0, p.Amount.Float64)
	assert.False(t, p.PricingVersion.Valid)
}

func TestCheckout_OfflineMembership_Bypassed(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	req := membershipCheckout(1, common.CurrencyNIS)
	req.PaymentType = null.StringFrom(common.PaymentTypeOffline)
	req.PaymentMethod = null.StringFrom("cash")
	got := POST_ROOT(t, a, "/v2/transaction/", req, http.StatusCreated)
	assert.NotNil(t, got["Payment"])
}

func TestCheckout_OfflineMembership_NonAdmin_Forbidden(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")

	req := membershipCheckout(1, common.CurrencyNIS)
	req.PaymentType = null.StringFrom(common.PaymentTypeOffline)
	req.PaymentMethod = null.StringFrom("cash")

	// The 403 response has no JSON body, so bypass the do() helper.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v2/transaction/", requestBody(t, req))
	ctx := context.WithValue(r.Context(), common.CtxAuthClaims, &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		Email:       "checkout@example.com",
		RealmAccess: middleware.Roles{Roles: []string{"some-role"}},
	})
	a.gEngine.ServeHTTP(w, r.WithContext(ctx))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, paymentsCount(t, a))
}

func TestCheckout_V2_DonationFetchError_Rejected(t *testing.T) {
	a := newCheckoutTestApp(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	createCheckoutAccount(t, a, "IL")

	// The 500 response has no JSON body, so bypass the do() helper.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v2/transaction/", requestBody(t, membershipCheckout(180, common.CurrencyNIS)))
	ctx := context.WithValue(r.Context(), common.CtxAuthClaims, &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{common.RoleRoot}},
	})
	a.gEngine.ServeHTTP(w, r.WithContext(ctx))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, 0, paymentsCount(t, a))
}
