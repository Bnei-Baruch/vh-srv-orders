package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

type checkoutCall struct{ path, auth string }

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
		calls = append(calls, checkoutCall{r.URL.Path, r.Header.Get("Authorization")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	client := pelecard.NewClient()
	client.BaseURL = srv.URL
	client.Tokens = keycloak.StaticTokenSource("tok")
	a.ordersAPI.checkout = client

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

	assert.Equal(t, "paid", got["status"])
	assert.Equal(t, []checkoutCall{{"/vh/payment/" + p.orderKey, "Bearer tok"}}, calls())
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

	assert.Equal(t, "already_paid", got["status"])
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

	notify(t, a, p, http.StatusUnprocessableEntity)

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

	for name, body := range map[string]gin.H{
		"no dash":             {"user_key": "ord", "reference": p.paramX},
		"not a number":        {"user_key": "ord-x", "reference": p.paramX},
		"wrong prefix":        {"user_key": p.paramX, "reference": p.orderKey},
		"another order's":     {"user_key": other.orderKey, "reference": p.paramX},
		"no such payment":     {"user_key": p.orderKey, "reference": "m-999999"},
		"missing reference":   {"user_key": p.orderKey},
		"negative payment id": {"user_key": p.orderKey, "reference": "m--1"},
		"zero payment id":     {"user_key": p.orderKey, "reference": "m-0"},
		"past int4":           {"user_key": p.orderKey, "reference": "m-4294967297"},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			raw, _ := json.Marshal(body)
			a.gEngine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/checkout/notify", strings.NewReader(string(raw))))
			assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, w.Code, w.Body.String())
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
