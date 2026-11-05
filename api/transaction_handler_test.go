package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

// A 403 is a valid key that is not allowed on /vh: no retry, and the payer is
// sent to their error page rather than handed the error JSON with a 200.
func Test_page_forbidden_is_not_retried_and_sends_the_error_url(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusForbidden, "application/json", `{"error":"forbidden"}`)

	w := postAsRoot(a, "/v2/transaction/new_token", `{"ErrorURL":"http://test/error"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"url":"http://test/error"}`, w.Body.String())
	assert.Len(t, calls(), 1)
}

// An order type with no page gets the error URL, and external_payments is not
// called at all.
func Test_checkout_unknown_type_sends_the_error_url_without_a_call(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	req := membershipCheckout(1, common.CurrencyUSD)
	req.ProductType = null.StringFrom("ticket")
	req.Type = null.StringFrom("weekly")
	req.TerminalId = null.StringFrom("")
	req.ErrorURL = null.StringFrom("http://test/error")
	got := POST_ROOT(t, a, "/v2/transaction/", req, http.StatusOK)

	assert.True(t, strings.HasPrefix(got["url"].(string), "http://test/error/"), got["url"])
	assert.Empty(t, calls())
}

// The payer still gets a 200 with their error URL, so the request log and the
// Sentry middleware never see a failure. reportPageFailure is the only trace
// left when the credential stops working.
func Test_page_failure_is_logged_at_error(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubPages(t, a, http.StatusUnauthorized, "application/json", `{"error":"unauthorized"}`)

	var log bytes.Buffer
	real := slog.Default()
	t.Cleanup(func() { slog.SetDefault(real) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))

	w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{"ErrorURL":"http://test/error"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var found map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "payment page failed" {
			found = rec
		}
	}
	require.NotNil(t, found, "the failure must be logged")
	assert.Equal(t, "ERROR", found["level"])
	assert.Contains(t, found["err"], pelecard.PageRenewCard)
	assert.Contains(t, found["err"], pelecard.ErrUnauthorized.Error())
}

// stubPageBodies is stubPages for tests that need what was posted: it records
// each request body.
func stubPageBodies(t *testing.T, a *App) func() []map[string]any {
	t.Helper()
	var (
		mu     sync.Mutex
		bodies []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://pelecard.example/page"}`))
	}))
	t.Cleanup(srv.Close)

	a.ordersAPI.pages = &pelecard.Client{
		Client:  resty.New(),
		BaseURL: srv.URL,
		Tokens:  keycloak.StaticTokenSource("tok"),
	}
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

// postAs sends a raw request as USER_KEY with the given realm roles.
func postAs(a *App, path, body string, roles ...string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	claims := &middleware.IDTokenClaims{Sub: USER_KEY, RealmAccess: middleware.Roles{Roles: roles}}
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	return w
}

// A member can open a card-update page only under their own key, whatever the
// body names: external_payments files the attempt and its result by UserKey.
func Test_new_token_member_cannot_name_another_user_key(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	bodies := stubPageBodies(t, a)

	w := postAs(a, "/v2/transaction/new_token", `{"UserKey":"someone-else","Reference":"new_token_1"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, bodies(), 1)
	assert.Equal(t, USER_KEY, bodies()[0]["UserKey"])
}

func Test_new_token_admin_keeps_the_user_key_from_the_body(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	bodies := stubPageBodies(t, a)

	w := postAs(a, "/v2/transaction/new_token", `{"UserKey":"someone-else","Reference":"new_token_1"}`, common.RoleAdmin)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, bodies(), 1)
	assert.Equal(t, "someone-else", bodies()[0]["UserKey"])
}
