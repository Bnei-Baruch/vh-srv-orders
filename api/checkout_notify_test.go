package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type checkoutCall struct{ path, reference, auth string }

// stubCheckout points the app's checkout client at a server answering every
// payment read with status and body, and returns the calls it received.
func stubCheckout(t *testing.T, a *App, status int, body any) func() []checkoutCall {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []checkoutCall
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, checkoutCall{r.URL.Path, r.URL.Query().Get("reference"), r.Header.Get("Authorization")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	client := pelecard.NewClient()
	client.BaseURL = srv.URL
	client.Tokens = keycloak.StaticTokenSource("tok")
	a.ordersAPI.pages = client

	return func() []checkoutCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]checkoutCall(nil), calls...)
	}
}

type pendingPayment struct {
	orderID, paymentID int
	orderKey, paramX   string
}

var seededPayers atomic.Int64

func seedPendingPayment(t *testing.T, a *App, orderType string) pendingPayment {
	t.Helper()
	payer := seededPayers.Add(1)
	pool := a.testPool(t)
	ctx := context.Background()
	var p pendingPayment
	var accountID int
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO accounts ("Email", "UserKey") VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("payer%d@test.test", payer), fmt.Sprintf("kc-payer-%d", payer)).Scan(&accountID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID", "Type", "Status", "Amount", "Currency") VALUES ($1, $2, 'new', 180, 'ILS') RETURNING id`,
		accountID, orderType).Scan(&p.orderID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO payments ("OrderID", "PaymentStatus", "Amount", "Currency") VALUES ($1, 'pending', 180, 'ILS') RETURNING id`,
		p.orderID).Scan(&p.paymentID))
	_, err := pool.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, p.paymentID)
	require.NoError(t, err)
	p.orderKey = fmt.Sprintf("ord-%d", p.orderID)
	p.paramX = fmt.Sprintf("m-%d", p.paymentID)
	return p
}

func gatewayRecord(p pendingPayment) pelecard.GatewayPayment {
	return pelecard.GatewayPayment{
		UserKey:                 p.orderKey,
		AdditionalDetailsParamX: p.paramX,
		TransactionId:           "tx-1",
		CreditCardNumber:        "458000******1234",
		CreditCardExpDate:       "1230",
		DebitTotal:              "18000",
		DebitCurrency:           "1",
		Token:                   "card-token",
		ApprovalNo:              "0123456",
	}
}

type paymentState struct {
	paymentStatus, token, authNo, orderStatus string
}

func readPaymentState(t *testing.T, a *App, p pendingPayment) paymentState {
	t.Helper()
	var s paymentState
	require.NoError(t, a.testPool(t).QueryRow(context.Background(), `
		SELECT COALESCE(p."PaymentStatus", ''), COALESCE(p.pelecard_token, ''), COALESCE(p."AuthNo", ''), COALESCE(o."Status", '')
		FROM payments p JOIN orders o ON o.id = p."OrderID" WHERE p.id = $1`, p.paymentID).
		Scan(&s.paymentStatus, &s.token, &s.authNo, &s.orderStatus))
	return s
}

func notify(t *testing.T, a *App, p pendingPayment, expectedCode int) gin.H {
	return POST(t, a, "/checkout/notify", gin.H{"user_key": p.orderKey, "reference": p.paramX}, expectedCode)
}

func Test_checkout_notify_marks_the_payment_paid_from_checkouts_record(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "recurring")
	calls := stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	got := notify(t, a, p, http.StatusOK)

	assert.Equal(t, "ok", got["status"])
	assert.Equal(t, []checkoutCall{{"/vh/payment/" + p.orderKey, p.paramX, "Bearer tok"}}, calls())
	assert.Equal(t, paymentState{"success", "card-token", "0123456", "paid"}, readPaymentState(t, a, p))
}

func Test_checkout_notify_for_a_paid_payment_changes_nothing_and_asks_nobody(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE payments SET "PaymentStatus" = 'success', pelecard_token = 'first' WHERE id = $1`, p.paymentID)
	require.NoError(t, err)
	calls := stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	got := notify(t, a, p, http.StatusOK)

	assert.Equal(t, "ok", got["status"])
	assert.Empty(t, calls())
	assert.Equal(t, "first", readPaymentState(t, a, p).token)
}

func Test_checkout_notify_before_checkout_has_a_valid_payment_is_409(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	stubCheckout(t, a, http.StatusNotFound, gin.H{"status": "error"})

	notify(t, a, p, http.StatusConflict)

	assert.Equal(t, paymentState{"pending", "", "", "new"}, readPaymentState(t, a, p))
}

func Test_checkout_notify_whose_record_names_another_payment_is_refused(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	record := gatewayRecord(p)
	record.AdditionalDetailsParamX = "m-999999"
	stubCheckout(t, a, http.StatusOK, record)

	got := notify(t, a, p, http.StatusUnprocessableEntity)

	// Nothing of checkout's record is echoed to a public caller.
	assert.Equal(t, "cannot confirm this payment", got["error"])

	assert.Equal(t, paymentState{"pending", "", "", "new"}, readPaymentState(t, a, p))
}

// Until checkout stores the token, a recurring order marked from its record
// would leave renewals nothing to charge. The browser return still carries it.
func Test_checkout_notify_for_a_recurring_order_without_a_token_waits(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "recurring")
	record := gatewayRecord(p)
	record.Token = ""
	stubCheckout(t, a, http.StatusOK, record)

	notify(t, a, p, http.StatusConflict)

	assert.Equal(t, "pending", readPaymentState(t, a, p).paymentStatus)
}

func Test_checkout_notify_for_a_regular_order_needs_no_token(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	record := gatewayRecord(p)
	record.Token = ""
	stubCheckout(t, a, http.StatusOK, record)

	notify(t, a, p, http.StatusOK)

	assert.Equal(t, "paid", readPaymentState(t, a, p).orderStatus)
}

func Test_checkout_notify_with_ids_that_name_no_payment_never_asks_checkout(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	other := seedPendingPayment(t, a, "regular")
	calls := stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	for name, tc := range map[string]struct {
		body gin.H
		want int
	}{
		// Missing field: the body does not bind.
		"missing reference": {gin.H{"user_key": p.orderKey}, http.StatusBadRequest},
		// Binds, but names no payment of that order.
		"no dash":             {gin.H{"user_key": "ord", "reference": p.paramX}, http.StatusUnprocessableEntity},
		"not a number":        {gin.H{"user_key": "ord-x", "reference": p.paramX}, http.StatusUnprocessableEntity},
		"wrong prefix":        {gin.H{"user_key": p.paramX, "reference": p.orderKey}, http.StatusUnprocessableEntity},
		"another order's":     {gin.H{"user_key": other.orderKey, "reference": p.paramX}, http.StatusUnprocessableEntity},
		"no such payment":     {gin.H{"user_key": p.orderKey, "reference": "m-999999"}, http.StatusUnprocessableEntity},
		"negative payment id": {gin.H{"user_key": p.orderKey, "reference": "m--1"}, http.StatusUnprocessableEntity},
		"zero payment id":     {gin.H{"user_key": p.orderKey, "reference": "m-0"}, http.StatusUnprocessableEntity},
		"past int4":           {gin.H{"user_key": p.orderKey, "reference": "m-2147483648"}, http.StatusUnprocessableEntity},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			raw, _ := json.Marshal(tc.body)
			a.gEngine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/checkout/notify", strings.NewReader(string(raw))))
			assert.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
	assert.Empty(t, calls())
}

// The notify route is registered before auth; everything after it stays
// behind auth.
func Test_checkout_notify_is_public_and_the_rest_stays_behind_auth(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	a.gEngine = gin.New()
	a.initRoutes(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	stubCheckout(t, a, http.StatusNotFound, gin.H{})

	for path, want := range map[string]int{
		"/checkout/notify": http.StatusBadRequest,
		"/orders/paid":     http.StatusUnauthorized,
	} {
		w := httptest.NewRecorder()
		a.gEngine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		assert.Equal(t, want, w.Code, path)
	}
}

// A browser return after checkout's notify confirmed the payment must not
// replace checkout's facts with the query string's.
func Test_paid_return_for_a_confirmed_payment_changes_nothing(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "recurring")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	notify(t, a, p, http.StatusOK)

	POST_ROOT(t, a, "/orders/paid", gin.H{
		"user_key": p.orderKey, "additional_details_param_x": p.paramX,
		"success": "1", "token": "from-the-browser", "authNo": "999",
	}, http.StatusOK)

	assert.Equal(t, paymentState{"success", "card-token", "0123456", "paid"}, readPaymentState(t, a, p))
}

// Renewals charge with both the token and the approval number, so a recurring
// order waits until checkout has both.
func Test_checkout_notify_for_a_recurring_order_without_an_approval_number_waits(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "recurring")
	record := gatewayRecord(p)
	record.ApprovalNo = ""
	stubCheckout(t, a, http.StatusOK, record)

	notify(t, a, p, http.StatusConflict)

	assert.Equal(t, "pending", readPaymentState(t, a, p).paymentStatus)
}

// writeHalfConfirm leaves p the way a confirm that wrote the payment and then
// failed before the order leaves it: payment successful, order still new.
func writeHalfConfirm(t *testing.T, a *App, p pendingPayment) {
	t.Helper()
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE payments SET "PaymentStatus" = 'success', success = '1', pelecard_token = 'first' WHERE id = $1`, p.paymentID)
	require.NoError(t, err)
}

// A retried notify finishes an order an earlier confirm left unsettled, and
// still rewrites none of the payment's facts.
func Test_checkout_notify_finishes_a_half_written_confirm(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	writeHalfConfirm(t, a, p)
	calls := stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	notify(t, a, p, http.StatusOK)

	assert.Equal(t, paymentState{"success", "first", "", "paid"}, readPaymentState(t, a, p))
	assert.Empty(t, calls())
}

// The browser return repairs it the same way.
func Test_paid_return_finishes_a_half_written_confirm(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	writeHalfConfirm(t, a, p)

	POST_ROOT(t, a, "/orders/paid", gin.H{
		"user_key": p.orderKey, "additional_details_param_x": p.paramX,
		"success": "1", "token": "from-the-browser", "authNo": "999",
	}, http.StatusOK)

	assert.Equal(t, paymentState{"success", "first", "", "paid"}, readPaymentState(t, a, p))
}

// A settled order is not settled again: its PaymentDate and status stay.
func Test_checkout_notify_leaves_a_settled_order_alone(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	writeHalfConfirm(t, a, p)
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE orders SET "Status" = 'cancelled', "PaymentDate" = '2026-01-02' WHERE id = $1`, p.orderID)
	require.NoError(t, err)
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	notify(t, a, p, http.StatusOK)

	assert.Equal(t, "cancelled", readPaymentState(t, a, p).orderStatus)
}

// Ids are read in exactly the form this environment builds them, suffix
// included; another environment's id is refused.
func Test_keyID_reads_only_this_environments_ids(t *testing.T) {
	t.Setenv("SUFX", "-G1")
	id, err := keyID("ord-5-G1", "ord")
	require.NoError(t, err)
	assert.Equal(t, 5, id)
	for _, key := range []string{"ord-5", "ord-5-G2", "ord-5-G1-x", "m-5-G1", "ord--5-G1", "ord-0-G1", "ord-4294967297-G1"} {
		_, err := keyID(key, "ord")
		assert.ErrorIs(t, err, common.ErrInvalidValues, key)
	}

	t.Setenv("SUFX", "")
	_, err = keyID("m-7-G1", "m")
	assert.ErrorIs(t, err, common.ErrInvalidValues, "a dev id on prod")
	id, err = keyID("m-7", "m")
	require.NoError(t, err)
	assert.Equal(t, 7, id)
}

// Known answer shared with external_payments' notify tests:
// printf '1.{}' | openssl dgst -sha256 -hmac key
const knownNotifySig = "v1=1ba6b8171186efc613e8bcc0cbdab2748f24984d7c5a84faa2637afa0e40d224"

func Test_verifyNotifySignature(t *testing.T) {
	at := time.Unix(1, 0)
	body := []byte("{}")
	for name, tc := range map[string]struct {
		body    []byte
		ts, sig string
		keys    []string
		now     time.Time
		want    error
	}{
		"valid":                {body, "1", knownNotifySig, []string{"key"}, at, nil},
		"second key, rotation": {body, "1", knownNotifySig, []string{"new", "key"}, at, nil},
		"edge of the window":   {body, "1", knownNotifySig, []string{"key"}, at.Add(notifyMaxSkew), nil},
		"no key configured":    {body, "1", knownNotifySig, nil, at, errNotifyNoKey},
		"no signature":         {body, "1", "", []string{"key"}, at, errNotifyUnsigned},
		"no timestamp":         {body, "", knownNotifySig, []string{"key"}, at, errNotifyUnsigned},
		"replayed later":       {body, "1", knownNotifySig, []string{"key"}, at.Add(notifyMaxSkew + time.Second), errNotifyStale},
		"from the future":      {body, "1", knownNotifySig, []string{"key"}, at.Add(-notifyMaxSkew - time.Second), errNotifyStale},
		"timestamp not a num":  {body, "x", knownNotifySig, []string{"key"}, at, errNotifyStale},
		"no v1 prefix":         {body, "1", knownNotifySig[3:], []string{"key"}, at, errNotifyBadFormat},
		"not hex":              {body, "1", "v1=zz", []string{"key"}, at, errNotifyBadFormat},
		"wrong key":            {body, "1", knownNotifySig, []string{"other"}, at, errNotifyMismatch},
		"body changed":         {[]byte(`{"a":1}`), "1", knownNotifySig, []string{"key"}, at, errNotifyMismatch},
		"timestamp changed":    {body, "2", knownNotifySig, []string{"key"}, at, errNotifyMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, verifyNotifySignature(tc.body, tc.ts, tc.sig, tc.keys, tc.now), tc.want)
			if tc.want == nil {
				assert.NoError(t, verifyNotifySignature(tc.body, tc.ts, tc.sig, tc.keys, tc.now))
			}
		})
	}
}

func signNotify(key string, ts int64, body []byte) (string, string) {
	t := strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(t + "."))
	mac.Write(body)
	return t, "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func enforceNotifyHMAC(t *testing.T, keys ...string) {
	t.Helper()
	oldKeys, oldEnforce := common.Config.CheckoutNotifyHMACKeys, common.Config.CheckoutNotifyHMACEnforce
	common.Config.CheckoutNotifyHMACKeys, common.Config.CheckoutNotifyHMACEnforce = keys, true
	t.Cleanup(func() {
		common.Config.CheckoutNotifyHMACKeys, common.Config.CheckoutNotifyHMACEnforce = oldKeys, oldEnforce
	})
}

func postNotify(a *App, p pendingPayment, headers map[string]string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(gin.H{"user_key": p.orderKey, "reference": p.paramX})
	r := httptest.NewRequest(http.MethodPost, "/checkout/notify", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.gEngine.ServeHTTP(w, r)
	return w
}

// Enforced: an unsigned or wrongly signed notice is refused before any lookup,
// with the same body as every other refusal.
func Test_checkout_notify_enforced_refuses_a_bad_signature(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	enforceNotifyHMAC(t, "k3y")
	p := seedPendingPayment(t, a, "regular")
	calls := stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	raw, _ := json.Marshal(gin.H{"user_key": p.orderKey, "reference": p.paramX})
	ts, wrong := signNotify("not-the-key", time.Now().Unix(), raw)

	for name, h := range map[string]map[string]string{
		"unsigned":  nil,
		"wrong key": {"X-Notify-Timestamp": ts, "X-Notify-Signature": wrong},
	} {
		w := postNotify(a, p, h)
		assert.Equal(t, http.StatusUnauthorized, w.Code, name)
		assert.JSONEq(t, `{"error":"cannot confirm this payment"}`, w.Body.String(), name)
	}
	assert.Empty(t, calls())
	assert.Equal(t, "pending", readPaymentState(t, a, p).paymentStatus)
}

func Test_checkout_notify_enforced_accepts_a_signed_notice(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	enforceNotifyHMAC(t, "k3y")
	p := seedPendingPayment(t, a, "regular")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	raw, _ := json.Marshal(gin.H{"user_key": p.orderKey, "reference": p.paramX})
	ts, sig := signNotify("k3y", time.Now().Unix(), raw)

	w := postNotify(a, p, map[string]string{"X-Notify-Timestamp": ts, "X-Notify-Signature": sig})

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "paid", readPaymentState(t, a, p).orderStatus)
}

// Not enforced (the switch-over step): an unsigned notice is still confirmed.
func Test_checkout_notify_not_enforced_confirms_an_unsigned_notice(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	enforceNotifyHMAC(t, "k3y")
	common.Config.CheckoutNotifyHMACEnforce = false
	p := seedPendingPayment(t, a, "regular")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	assert.Equal(t, http.StatusOK, postNotify(a, p, nil).Code)
	assert.Equal(t, "paid", readPaymentState(t, a, p).orderStatus)
}

// If the payment cannot be read, the browser return stops with 500. Treating
// the failed read as "not paid" would write the browser's facts over a payment
// checkout's notify already confirmed.
func Test_paid_return_when_the_payment_cannot_be_read_writes_nothing(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	notify(t, a, p, http.StatusOK)
	// Breaks GetPaymentByID's read only; UpdatePayment does not select it.
	_, err := a.testPool(t).Exec(context.Background(), `ALTER TABLE payments RENAME COLUMN pricing_version TO pricing_version_gone`)
	require.NoError(t, err)

	POST_ROOT(t, a, "/orders/paid", gin.H{
		"user_key": p.orderKey, "additional_details_param_x": p.paramX,
		"success": "1", "token": "from-the-browser", "authNo": "999",
	}, http.StatusInternalServerError)

	assert.Equal(t, paymentState{"success", "card-token", "0123456", "paid"}, readPaymentState(t, a, p))
}

// A member sends their own order's user_key with another order's payment. The
// payment must not be found from the reference alone: the other order stays
// unsettled, and the answer does not depend on whether that payment succeeded.
func Test_paid_return_with_another_orders_payment_is_refused(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	mine := seedPendingPayment(t, a, "regular")
	theirs := seedPendingPayment(t, a, "regular")
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE accounts SET "UserKey" = $1 WHERE id = (SELECT "AccountID" FROM orders WHERE id = $2)`, USER_KEY, mine.orderID)
	require.NoError(t, err)
	body := gin.H{"user_key": mine.orderKey, "additional_details_param_x": theirs.paramX, "success": "1"}

	// Pending: refused.
	POST(t, a, "/orders/paid", body, http.StatusUnprocessableEntity)

	// Succeeded, order left unsettled: refused the same way, nothing settled.
	writeHalfConfirm(t, a, theirs)
	POST(t, a, "/orders/paid", body, http.StatusUnprocessableEntity)
	assert.Equal(t, "new", readPaymentState(t, a, theirs).orderStatus)
}

// The number is written by FormatUint, so a sign or a leading zero is not an
// id this service built; int4 ends at 2^31-1.
func Test_keyID_takes_only_plain_digits_within_int4(t *testing.T) {
	t.Setenv("SUFX", "")
	id, err := keyID("ord-2147483647", "ord")
	require.NoError(t, err)
	assert.Equal(t, 2147483647, id)
	for _, key := range []string{"ord-2147483648", "ord-+5", "ord-007", "ord-0", "ord- 5", "ord-5 ", "ord-"} {
		_, err := keyID(key, "ord")
		assert.ErrorIs(t, err, common.ErrInvalidValues, key)
	}
}

// Every route registered after auth is behind it; only the notify is not.
// Walks the router, so a route added later is covered without editing this.
func Test_every_route_but_the_notify_is_behind_auth(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	a.gEngine = gin.New()
	a.initRoutes(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })

	routes := a.gEngine.Routes()
	require.NotEmpty(t, routes)
	for _, r := range routes {
		path := r.Path
		for _, seg := range strings.Split(r.Path, "/") {
			if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
				path = strings.Replace(path, seg, "1", 1)
			}
		}
		w := httptest.NewRecorder()
		a.gEngine.ServeHTTP(w, httptest.NewRequest(r.Method, path, strings.NewReader("{}")))
		if r.Path == "/checkout/notify" {
			assert.NotEqual(t, http.StatusUnauthorized, w.Code, "the notify must stay public")
			continue
		}
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", r.Method, r.Path)
	}
}

// Ids that another parser would read as the same order or payment, but that
// this service never built, are refused before anything reads them. Before,
// "ord-1-x" skipped the already-paid guard and the browser's token replaced
// checkout's on a confirmed payment.
func Test_paid_return_with_ids_in_another_form_is_refused(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "recurring")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	notify(t, a, p, http.StatusOK)

	for _, ids := range [][2]string{
		{p.orderKey + "-x", p.paramX},
		{fmt.Sprintf("ord-0%d", p.orderID), p.paramX},
		{p.orderKey, p.paramX + "-x"},
		{p.orderKey, fmt.Sprintf("m-0%d", p.paymentID)},
		{p.orderKey, ""},
	} {
		POST_ROOT(t, a, "/orders/paid", gin.H{
			"user_key": ids[0], "additional_details_param_x": ids[1],
			"success": "1", "token": "from-the-browser", "authNo": "999",
		}, http.StatusUnprocessableEntity)
	}

	assert.Equal(t, paymentState{"success", "card-token", "0123456", "paid"}, readPaymentState(t, a, p))
}

// Every field checkout records is carried into the update, each to its own
// column, and success is implied.
func Test_paidFromGateway_maps_every_field(t *testing.T) {
	got := paidFromGateway(&pelecard.GatewayPayment{
		UserKey: "ord-1", AdditionalDetailsParamX: "m-2", TransactionId: "tx",
		CardHebrewName: "heb", CreditCardAbroadCard: "abroad", CreditCardBrand: "brand",
		CreditCardCompanyClearer: "clearer", CreditCardCompanyIssuer: "issuer", CreditType: "ctype",
		CreditCardNumber: "4580", CreditCardExpDate: "1230", DebitCode: "dcode", DebitCurrency: "1",
		DebitTotal: "18000", DebitType: "dtype", FirstPaymentTotal: "first", FixedPaymentTotal: "fixed",
		TotalPayments: "3", JParam: "4", TransactionInitTime: "init", TransactionPelecardId: "pid",
		TransactionUpdateTime: "upd", VoucherId: "v-1", Token: "tok", ApprovalNo: "0123456",
	})
	s := null.StringFrom
	assert.Equal(t, repo.RequestPaid{
		UserKey: s("ord-1"), ParamX: s("m-2"), Success: s("1"), TransactionID: s("tx"),
		Token: s("tok"), AuthNo: s("0123456"), CardHebrewName: s("heb"), CCAbroadCard: s("abroad"),
		CCBrand: s("brand"), CCCompanyClearer: s("clearer"), CreditType: s("ctype"),
		CCNumber: s("4580"), CCExpDate: s("1230"), DebitCode: s("dcode"), DebitCurrency: s("1"),
		DebitTotal: s("18000"), DebitType: s("dtype"), FirstPaymentTotal: s("first"),
		FixedPaymentTotal: s("fixed"), TotalPayments: s("3"), JParam: s("4"),
		TransactionInitTime: s("init"), TransactionUpdateTime: s("upd"), VoucherID: s("v-1"),
	}, got)

	// An empty field is left out, not written as "".
	assert.False(t, paidFromGateway(&pelecard.GatewayPayment{UserKey: "ord-1"}).VoucherID.Valid)
}

// What the notify stores is checkout's record, field by field.
func Test_checkout_notify_stores_checkouts_facts(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	record := gatewayRecord(p)
	record.VoucherId = "v-77"
	stubCheckout(t, a, http.StatusOK, record)

	notify(t, a, p, http.StatusOK)

	var tx, ccNumber, ccExp, debitTotal, voucher string
	require.NoError(t, a.testPool(t).QueryRow(context.Background(), `
		SELECT COALESCE("TransactionID", ''), COALESCE("CCNumber", ''), COALESCE("CCExpDate", ''),
		       COALESCE("DebitTotal", ''), COALESCE("VoucherID", '')
		FROM payments WHERE id = $1`, p.paymentID).Scan(&tx, &ccNumber, &ccExp, &debitTotal, &voucher))
	assert.Equal(t, []string{"tx-1", "458000******1234", "1230", "18000", "v-77"},
		[]string{tx, ccNumber, ccExp, debitTotal, voucher})
}

type failingTokens struct{}

func (failingTokens) Token() (string, error) { return "", errors.New("keycloak down") }
func (failingTokens) Invalidate()            {}
func (failingTokens) InvalidateToken(string) {}

// Checkout refusing our bearer, or no bearer to be had, is 502 (retry later);
// anything else that goes wrong reading checkout is 500.
func Test_checkout_notify_maps_checkout_read_failures(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		tokens keycloak.TokenSource
		want   int
	}{
		"bearer refused":   {http.StatusUnauthorized, `{}`, keycloak.StaticTokenSource("tok"), http.StatusBadGateway},
		"no bearer":        {http.StatusOK, `{}`, failingTokens{}, http.StatusBadGateway},
		"malformed record": {http.StatusOK, `{"UserKey":`, keycloak.StaticTokenSource("tok"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			a := NewTestApp(t)
			defer CloseTestApp(a)
			p := seedPendingPayment(t, a, "regular")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			client := pelecard.NewClient()
			client.BaseURL = srv.URL
			client.Tokens = tc.tokens
			a.ordersAPI.pages = client

			notify(t, a, p, tc.want)

			assert.Equal(t, "pending", readPaymentState(t, a, p).paymentStatus)
		})
	}
}

// A payment marked successful whose success flag is not "1" is not rewritten,
// and its order is not settled: UpdateOrderAfterPayment would mark it failed.
func Test_checkout_notify_leaves_an_inconsistent_payment_alone(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE payments SET "PaymentStatus" = 'success', pelecard_token = 'first' WHERE id = $1`, p.paymentID)
	require.NoError(t, err)
	calls := stubCheckout(t, a, http.StatusOK, gatewayRecord(p))

	notify(t, a, p, http.StatusOK)

	assert.Equal(t, paymentState{"success", "first", "", "new"}, readPaymentState(t, a, p))
	assert.Empty(t, calls())
}
