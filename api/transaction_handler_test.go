package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aarondl/null/v9"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Bnei-Baruch/vh-srv-orders/api/middleware"
	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/keycloak"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/pelecard"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

// pageCall is one request the stub external_payments received.
type pageCall struct {
	path    string
	auth    string
	userKey string
}

// stubPages points the page client at a stub external_payments that answers
// every request with status, contentType and body, and records what it was
// sent, including the UserKey in the body. The token is fixed, so no Keycloak
// is needed.
func stubPages(t *testing.T, a *App, status int, contentType, body string) func() []pageCall {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []pageCall
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent struct{ UserKey string }
		_ = json.NewDecoder(r.Body).Decode(&sent)
		mu.Lock()
		calls = append(calls, pageCall{path: r.URL.Path, auth: r.Header.Get("Authorization"), userKey: sent.UserKey})
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

// postAs sends a raw request as USER_KEY with the given realm roles, without
// do()'s JSON decoding: some responses under test have no body.
func postAs(a *App, path, body string, roles ...string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	claims := &middleware.IDTokenClaims{Sub: USER_KEY, RealmAccess: middleware.Roles{Roles: roles}}
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	return w
}

func postAsRoot(a *App, path, body string) *httptest.ResponseRecorder {
	return postAs(a, path, body, common.RoleRoot)
}

// captureRequestLog records what is logged through slog.Default(), such as the
// logging middleware's request record.
func captureRequestLog(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	// slog.SetDefault also points the std log package at the new handler, and
	// setting slog's default back does not undo that, so restore log as well.
	real, w, f := slog.Default(), log.Writer(), log.Flags()
	t.Cleanup(func() { slog.SetDefault(real); log.SetOutput(w); log.SetFlags(f) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	return &buf
}

// requestErrors returns the errs of the logged "request" record. They are what
// the Sentry middleware captures on a 5xx.
func requestErrors(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["msg"] == "request" {
			assert.Equal(t, "ERROR", rec["level"])
			errs, _ := json.Marshal(rec["errs"])
			return string(errs)
		}
	}
	t.Fatal("no request record logged")
	return ""
}

// When the checkout service answers with something that isn't JSON (a proxy
// error, a maintenance page), the caller must get a 500, not a 200 with a null
// body that looks like success, and the cause must be recorded on the request
// so it reaches Sentry.
func Test_new_token_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubPages(t, a, http.StatusBadGateway, "text/html", "<html>502 Bad Gateway</html>")
	buf := captureRequestLog(t)

	POST_ROOT(t, a, "/v2/transaction/new_token", gin.H{}, http.StatusInternalServerError)

	assert.Contains(t, requestErrors(t, buf), "handleTransactionNewToken json.Unmarshal:")
}

func Test_new_token_no_cvv_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubPages(t, a, http.StatusBadGateway, "text/html", "<html>502 Bad Gateway</html>")
	buf := captureRequestLog(t)

	POST_ROOT(t, a, "/v2/transaction/new_token_no_cvv", gin.H{}, http.StatusInternalServerError)

	assert.Contains(t, requestErrors(t, buf), "handleTransactionNewTokenNoCVV json.Unmarshal:")
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

// The back office once sent 23 renewals for one order in 9 seconds, and each
// opened its own Pelecard page. Repeated clicks now get the first page.
func Test_new_token_no_cvv_repeated_clicks_open_one_page(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	for range 5 {
		w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{"Reference":"new_token_112447"}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.JSONEq(t, `{"url":"https://pelecard.example/page"}`, w.Body.String())
	}

	assert.Len(t, calls(), 1)
}

// Back from Pelecard after Cancel or a decline, the back office is on its
// status=failed URL and builds the next request's return URLs from it. That
// request must open a new page: the first one is closed at Pelecard.
func Test_new_token_no_cvv_after_cancel_opens_a_new_page(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	postAsRoot(a, "/v2/transaction/new_token_no_cvv",
		`{"Reference":"new_token_112447","CancelURL":"https://bo.example/customers/7?status=failed"}`)
	postAsRoot(a, "/v2/transaction/new_token_no_cvv",
		`{"Reference":"new_token_112447","CancelURL":"https://bo.example/customers/7?status=failed?status=failed"}`)

	assert.Len(t, calls(), 2)
}

func Test_new_token_no_cvv_other_orders_get_their_own_page(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{"Reference":"new_token_1"}`)
	postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{"Reference":"new_token_2"}`)

	assert.Len(t, calls(), 2)
}

// Once the card is saved, the page that saved it is used up: the next click
// for the order must open a new one.
func Test_new_token_no_cvv_saved_card_releases_the_page(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)
	got := POST_ROOT(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom("kc-renew")}, http.StatusCreated)
	accountID := int(got["data"].(float64))
	got = POST_ROOT(t, a, "/v2/order/", repo.Order{Amount: null.Float64From(15), AccountID: null.IntFrom(accountID)}, http.StatusCreated)
	orderID := int(got["data"].(float64))
	renew := fmt.Sprintf(`{"Reference":"new_token_%d"}`, orderID)

	postAsRoot(a, "/v2/transaction/new_token_no_cvv", renew)
	POST_ROOT(t, a, "/v2/order/update_token", repo.RequestUpdateToken{
		CardExp: "0627", CardNumber: "1234123412341234", OrderId: orderID, Token: "renewed-token",
		ParamX: fmt.Sprintf("new_token_%d", orderID),
	}, http.StatusOK)
	postAsRoot(a, "/v2/transaction/new_token_no_cvv", renew)

	assert.Len(t, calls(), 2)
}

// A failed page is not handed to the next click: checkout answers 502 once,
// then a page, and the second click gets the page.
func Test_new_token_no_cvv_failed_page_is_retried(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://pelecard.example/page"}`))
	}))
	t.Cleanup(srv.Close)
	a.ordersAPI.pages = &pelecard.Client{Client: resty.New(), BaseURL: srv.URL, Tokens: keycloak.StaticTokenSource("tok")}

	POST_ROOT(t, a, "/v2/transaction/new_token_no_cvv", gin.H{"Reference": "new_token_9"}, http.StatusInternalServerError)
	w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{"Reference":"new_token_9"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"url":"https://pelecard.example/page"}`, w.Body.String())
	assert.Equal(t, int32(2), calls.Load())
}

func Test_recurring_checkout_opens_the_vh_token_page_with_a_bearer(t *testing.T) {
	a := newCheckoutTestApp(t, emptyPriority)
	createCheckoutAccount(t, a, "IL")
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	req := membershipCheckout(180, common.CurrencyNIS)
	req.TerminalId = null.StringFrom("")
	got := POST_ROOT(t, a, "/v2/transaction/", req, http.StatusOK)

	assert.Equal(t, "https://pelecard.example/page", got["url"])
	assert.Equal(t, []pageCall{{path: "/vh/token/new", auth: "Bearer tok", userKey: "ord-1"}}, calls())
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
	assert.Equal(t, []pageCall{{path: "/vh/emv/new", auth: "Bearer tok", userKey: "ord-1"}}, calls())
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

	buf := captureRequestLog(t)

	w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{"ErrorURL":"http://test/error"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var found map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
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

// A member can open a card-update page only under their own key, whatever the
// body names: external_payments files the attempt and its result by UserKey.
func Test_new_token_member_cannot_name_another_user_key(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	w := postAs(a, "/v2/transaction/new_token", `{"UserKey":"someone-else","Reference":"new_token_1"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, calls(), 1)
	assert.Equal(t, USER_KEY, calls()[0].userKey)
}

func Test_new_token_admin_keeps_the_user_key_from_the_body(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	calls := stubPages(t, a, http.StatusOK, "application/json", `{"url":"https://pelecard.example/page"}`)

	w := postAs(a, "/v2/transaction/new_token", `{"UserKey":"someone-else","Reference":"new_token_1"}`, common.RoleAdmin)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, calls(), 1)
	assert.Equal(t, "someone-else", calls()[0].userKey)
}

// seedTransaction stores an account keyed userKey with one order, payment and
// transaction, and returns the transaction id.
func seedTransaction(t *testing.T, a *App, userKey string) int {
	t.Helper()
	pool := a.testPool(t)
	ctx := context.Background()
	var accountID, orderID, paymentID, txID int
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO accounts ("Email", "UserKey") VALUES ($1, $2) RETURNING id`, userKey+"@test.test", userKey).Scan(&accountID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "Type", "Status", "Amount", "Currency") VALUES ($1, 'regular', 'new', 180, 'ILS') RETURNING id`,
		accountID).Scan(&orderID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO payments ("OrderID", "PaymentStatus", "Amount", "Currency") VALUES ($1, 'pending', 180, 'ILS') RETURNING id`,
		orderID).Scan(&paymentID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO transaction (order_id, payment_id, account_id, terminal_id) VALUES ($1, $2, $3, 'ben_regular_pelecard') RETURNING id`,
		orderID, paymentID, accountID).Scan(&txID))
	return txID
}

// A member reads their own transaction and not another member's. The account
// filter used to be formatted from a *int, so it held the pointer's address
// and nobody's lookup ever matched, an admin's included.
func Test_get_transaction_is_scoped_to_the_callers_account(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	mine := seedTransaction(t, a, USER_KEY)
	theirs := seedTransaction(t, a, "kc-someone-else")

	got := GET(t, a, fmt.Sprintf("/v2/transaction/%d", mine), http.StatusOK)
	assert.Equal(t, float64(mine), got["data"].(map[string]any)["id"])

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v2/transaction/%d", theirs), nil)
	claims := &middleware.IDTokenClaims{Sub: USER_KEY, RealmAccess: middleware.Roles{Roles: []string{"some-role"}}}
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func Test_get_transaction_as_admin_reads_any(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	theirs := seedTransaction(t, a, "kc-someone-else")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v2/transaction/%d", theirs), nil)
	claims := &middleware.IDTokenClaims{Sub: USER_KEY, RealmAccess: middleware.Roles{Roles: []string{common.RoleRoot}}}
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// transaction.id is int4, so an id past its range is refused up front rather
// than reaching pgx, which cannot encode it and answers 500.
func Test_get_transaction_id_beyond_int4_is_400(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	GET(t, a, "/v2/transaction/2147483648", http.StatusBadRequest)
}
