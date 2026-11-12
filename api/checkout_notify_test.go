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
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
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

// A successful payment is written once: a second confirm, or a late failure,
// is refused by the write itself and changes nothing.
func Test_UpdatePayment_writes_a_successful_payment_once(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	ctx := eventstest.WithTestEventBuilder(t, context.Background())
	_, err := a.repo.UpdatePayment(ctx, paidFromGateway(&pelecard.GatewayPayment{
		UserKey: p.orderKey, AdditionalDetailsParamX: p.paramX, Token: "first", ApprovalNo: "111",
	}))
	require.NoError(t, err)

	for name, rp := range map[string]repo.RequestPaid{
		"second success": paidFromGateway(&pelecard.GatewayPayment{
			UserKey: p.orderKey, AdditionalDetailsParamX: p.paramX, Token: "second", ApprovalNo: "222"}),
		"late failure": {UserKey: null.StringFrom(p.orderKey), ParamX: null.StringFrom(p.paramX), Success: null.StringFrom("0")},
	} {
		_, err := a.repo.UpdatePayment(ctx, rp)
		assert.ErrorIs(t, err, common.ErrAlreadyPaid, name)
	}
	s := readPaymentState(t, a, p)
	assert.Equal(t, "success", s.paymentStatus)
	assert.Equal(t, "first", s.token)
	assert.Equal(t, "111", s.authNo)
}

// A failed browser return after the payment succeeded no longer flips it.
func Test_paid_return_with_a_late_failure_keeps_the_payment_successful(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	notify(t, a, p, http.StatusOK)

	POST_ROOT(t, a, "/orders/paid", gin.H{
		"user_key": p.orderKey, "additional_details_param_x": p.paramX, "success": "0",
	}, http.StatusOK)

	assert.Equal(t, paymentState{"success", "card-token", "0123456", "paid"}, readPaymentState(t, a, p))
}

// The browser return confirms the payment while the notify is reading
// checkout. The notify's write is refused, it answers 200, keeps the browser's
// facts, and still settles the order.
func Test_checkout_notify_racing_a_browser_return_writes_nothing(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := a.testPool(t).Exec(context.Background(),
			`UPDATE payments SET "PaymentStatus" = 'success', success = '1', pelecard_token = 'browser' WHERE id = $1`, p.paymentID)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gatewayRecord(p))
	}))
	t.Cleanup(srv.Close)
	client := pelecard.NewClient()
	client.BaseURL = srv.URL
	client.Tokens = keycloak.StaticTokenSource("tok")
	a.ordersAPI.pages = client

	notify(t, a, p, http.StatusOK)

	assert.Equal(t, paymentState{"success", "browser", "", "paid"}, readPaymentState(t, a, p))
}

// The payments write and the payments_pelecard write commit together. If the
// second fails, neither is kept, and a retry writes both.
func Test_UpdatePayment_rolls_back_both_writes_when_one_fails(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	pool := a.testPool(t)
	ctx := eventstest.WithTestEventBuilder(t, context.Background())
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_pelecard_update() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN RAISE EXCEPTION 'pelecard write failed'; END $$;
		CREATE TRIGGER fail_pelecard BEFORE UPDATE ON payments_pelecard
		FOR EACH ROW EXECUTE FUNCTION fail_pelecard_update();`)
	require.NoError(t, err)
	record := gatewayRecord(p)
	rp := paidFromGateway(&record)

	_, err = a.repo.UpdatePayment(ctx, rp)
	require.Error(t, err)
	assert.NotErrorIs(t, err, common.ErrAlreadyPaid)
	assert.Equal(t, "pending", readPaymentState(t, a, p).paymentStatus)

	_, err = pool.Exec(ctx, `DROP TRIGGER fail_pelecard ON payments_pelecard`)
	require.NoError(t, err)
	_, err = a.repo.UpdatePayment(ctx, rp)
	require.NoError(t, err)
	assert.Equal(t, "success", readPaymentState(t, a, p).paymentStatus)
}

// A late browser return carrying an error, for a payment checkout's notify
// already confirmed, is done: 200, and nothing changes.
func Test_paid_return_with_an_error_for_a_confirmed_payment_is_done(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")
	stubCheckout(t, a, http.StatusOK, gatewayRecord(p))
	notify(t, a, p, http.StatusOK)

	POST_ROOT(t, a, "/orders/paid", gin.H{
		"user_key": p.orderKey, "additional_details_param_x": p.paramX, "error": "301",
	}, http.StatusOK)

	assert.Equal(t, paymentState{"success", "card-token", "0123456", "paid"}, readPaymentState(t, a, p))
}

// Without a confirmed payment, a return carrying an error is still refused.
func Test_paid_return_with_an_error_for_a_pending_payment_is_refused(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	p := seedPendingPayment(t, a, "regular")

	POST_ROOT(t, a, "/orders/paid", gin.H{
		"user_key": p.orderKey, "additional_details_param_x": p.paramX, "error": "301",
	}, http.StatusUnprocessableEntity)

	assert.Equal(t, "pending", readPaymentState(t, a, p).paymentStatus)
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
