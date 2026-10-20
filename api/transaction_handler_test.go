package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// pageCall is one request the stub external_payments received.
type pageCall struct {
	path string
	auth string
}

// stubPages points the page client at a stub external_payments that answers
// every request with status, contentType and body, and records what it was
// sent. The token is fixed, so no Keycloak is needed.
func stubPages(t *testing.T, a *App, status int, contentType, body string) func() []pageCall {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []pageCall
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, pageCall{path: r.URL.Path, auth: r.Header.Get("Authorization")})
		mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	a.ordersAPI.pages = &pelecard.Client{
		Client:  resty.New(),
		BaseURL: srv.URL,
		Tokens:  keycloak.StaticTokenSource("tok"),
	}
	return func() []pageCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]pageCall(nil), calls...)
	}
}

// postAsRoot sends a raw request, without do()'s JSON decoding: some responses
// under test have no body.
func postAsRoot(a *App, path string, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	claims := &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{common.RoleRoot}},
	}
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	return w
}

// When the checkout service answers with something that isn't JSON (a proxy
// error, a maintenance page), the caller must get a 500, not a 200 with a null
// body that looks like success.
func Test_new_token_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubPages(t, a, http.StatusBadGateway, "text/html", "<html>502 Bad Gateway</html>")

	w := postAsRoot(a, "/v2/transaction/new_token", `{}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.NotEqual(t, "null", w.Body.String())
}

func Test_new_token_no_cvv_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubPages(t, a, http.StatusBadGateway, "text/html", "<html>502 Bad Gateway</html>")

	w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.NotEqual(t, "null", w.Body.String())
}

// Every page call carries a bearer, to the /vh route: the shared routes read
// no credential, so external_payments logged these as requested_by=anonymous.
func Test_new_token_opens_the_vh_page_with_a_bearer(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	w := postAsRoot(a, "/v2/transaction/new_token", `{}`)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []pageCall{{path: "/vh/emv/new_token", auth: "Bearer tok"}}, calls())
}

func Test_new_token_no_cvv_opens_the_vh_page_with_a_bearer(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{}`)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []pageCall{{path: "/vh/renew-card", auth: "Bearer tok"}}, calls())
}

func Test_recurring_checkout_opens_the_vh_token_page_with_a_bearer(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	req := membershipCheckout(180, common.CurrencyNIS)
	req.TerminalId = null.StringFrom("")
	got := POST_ROOT(t, a, "/v2/transaction/", req, http.StatusOK)

	assert.Equal(t, "https://pelecard.example/page", got["url"])
	assert.Equal(t, []pageCall{{path: "/vh/token/new", auth: "Bearer tok"}}, calls())
}

func Test_regular_checkout_opens_the_vh_emv_page_with_a_bearer(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")
	calls := stubPages(t, a, http.StatusOK, "application/json",
		`{"status":"success","url":"https://pelecard.example/page"}`)

	req := membershipCheckout(1, common.CurrencyUSD)
	req.ProductType = null.StringFrom("ticket")
	req.Type = null.StringFrom("regular")
	req.TerminalId = null.StringFrom("")
	got := POST_ROOT(t, a, "/v2/transaction/", req, http.StatusOK)

	assert.Equal(t, "https://pelecard.example/page", got["url"])
	assert.Equal(t, []pageCall{{path: "/vh/emv/new", auth: "Bearer tok"}}, calls())
}

// A rejected credential is retried once with a fresh token; if it is rejected
// again, the payer is sent to their error page rather than shown the 401.
func Test_page_rejected_credential_is_retried_once_then_sends_the_error_url(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusUnauthorized, "application/json", `{"error":"unauthorized"}`)

	w := postAsRoot(a, "/v2/transaction/new_token", `{"ErrorURL":"http://test/error"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"url":"http://test/error"}`, w.Body.String())
	assert.Len(t, calls(), 2, "one retry with a fresh token")
}
