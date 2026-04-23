package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func TestHandleMonthlyPriceByKCID_UnknownKeycloakID(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := do(t, a, "GET", "/v2/pricing/monthly/nonexistent-kc-id", nil, http.StatusBadRequest, DoOptions{isRoot: true})
	assert.Equal(t, false, got["success"])
}

func TestHandleMonthlyPriceByKCID_V1_USD(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)

	got := GET(t, a, fmt.Sprintf("/v2/pricing/monthly/%s?pricing_version=v1&currency=usd", USER_KEY), http.StatusOK)
	assert.Equal(t, true, got["success"])
	data := got["data"].(map[string]interface{})
	assert.Equal(t, 20.0, data["amount"])
	assert.Equal(t, common.CurrencyUSD, data["currency"])
	assert.Equal(t, "v1", data["pricing_version"])
	assert.Nil(t, data["v2_details"])
}

func TestHandleMonthlyPriceByKCID_V1_EUR(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)

	got := GET(t, a, fmt.Sprintf("/v2/pricing/monthly/%s?pricing_version=v1&currency=EUR", USER_KEY), http.StatusOK)
	data := got["data"].(map[string]interface{})
	assert.Equal(t, 20.0, data["amount"])
	assert.Equal(t, common.CurrencyEUR, data["currency"])
}

func TestHandleMonthlyPriceByKCID_V1_FallbackToUSDForUnknownCurrency(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)

	got := GET(t, a, fmt.Sprintf("/v2/pricing/monthly/%s?pricing_version=v1&currency=GBP", USER_KEY), http.StatusOK)
	data := got["data"].(map[string]interface{})
	assert.Equal(t, 20.0, data["amount"])
	assert.Equal(t, common.CurrencyUSD, data["currency"])
}

func TestHandleMonthlyPriceByKCID_T1_NonILUsesV1(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{
		UserKey: null.StringFrom(USER_KEY),
		Country: null.StringFrom("US"),
	}, http.StatusCreated)

	got := GET(t, a, fmt.Sprintf("/v2/pricing/monthly/%s?pricing_version=t1&currency=USD", USER_KEY), http.StatusOK)
	data := got["data"].(map[string]interface{})
	assert.Equal(t, 20.0, data["amount"])
	assert.Nil(t, data["v2_details"])
}

func TestHandleMonthlyPriceByKCID_NonAdminCannotAccessOtherUser(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom("other-user")}, http.StatusCreated)

	// Non-admin querying a different user's ID gets 403 (no JSON body).
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v2/pricing/monthly/other-user?pricing_version=v1&currency=USD", nil)
	ctx := context.WithValue(r.Context(), common.CtxAuthClaims, &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{"some-role"}},
	})
	a.gEngine.ServeHTTP(w, r.WithContext(ctx))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleMonthlyPriceByKCID_AdminCanQueryAnyUser(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom("other-user")}, http.StatusCreated)

	got := do(t, a, "GET", "/v2/pricing/monthly/other-user?pricing_version=v1&currency=USD", nil, http.StatusOK, DoOptions{isRoot: true})
	assert.Equal(t, true, got["success"])
	data := got["data"].(map[string]interface{})
	assert.Equal(t, 20.0, data["amount"])
}

func TestHandleMonthlyPriceByKCID_DonationFetchError_ReturnsDegradedResponse(t *testing.T) {
	priorityServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer priorityServer.Close()

	origURL := common.Config.PriorityBaseURL
	common.Config.PriorityBaseURL = priorityServer.URL
	defer func() { common.Config.PriorityBaseURL = origURL }()

	a := NewTestApp(t)
	defer CloseTestApp(a)

	a.ordersAPI.SetPriorityClient(priority.NewClient())
	a.ordersAPI.SetProfileService(&notFoundProfileService{})

	POST_ROOT(t, a, "/v2/account/", repo.Account{
		UserKey: null.StringFrom(USER_KEY),
		Country: null.StringFrom("IL"),
		Email:   null.StringFrom("test@example.com"),
	}, http.StatusCreated)

	got := GET(t, a, fmt.Sprintf("/v2/pricing/monthly/%s", USER_KEY), http.StatusOK)
	assert.Equal(t, true, got["success"])
	data := got["data"].(map[string]interface{})
	assert.Equal(t, 180.0, data["amount"])
	assert.Equal(t, common.CurrencyNIS, data["currency"])
	assert.Equal(t, "v2", data["pricing_version"])
	v2Details := data["v2_details"].(map[string]interface{})
	discounts := v2Details["discounts"].([]interface{})
	require.Len(t, discounts, 1)
	d := discounts[0].(map[string]interface{})
	assert.Equal(t, true, d["error"])
	assert.Equal(t, false, d["eligible"])
}

// notFoundProfileService is a test stub that returns profiles.ErrNotFound for all calls.
type notFoundProfileService struct{}

func (s *notFoundProfileService) LookupProfile(_ context.Context, _ string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}

func (s *notFoundProfileService) LookupProfileByKeycloakId(_ context.Context, _ string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}

func (s *notFoundProfileService) GetProfileByKeycloakID(_ context.Context, _ string) (*profiles.Profile, error) {
	return nil, profiles.ErrNotFound
}
