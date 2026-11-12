package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func skipUpload(buffer []byte, fileName string) (string, error) {
	return "http://mock.url", nil
}

func Test_offline_payment(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	// Mock upload.
	realUpload := utils.UploadFileToS3
	t.Cleanup(func() { utils.UploadFileToS3 = realUpload })
	utils.UploadFileToS3 = skipUpload

	accountReq := repo.Account{UserKey: null.StringFrom(USER_KEY)}
	got := POST(t, a, "/v2/account/", accountReq, http.StatusCreated)
	fmt.Printf("Added account: %+v\n", got)

	offline_payment := repo.OfflinePaymentRequest{
		KeycloakID:    null.StringFrom(USER_KEY),
		Currency:      null.StringFrom("usd"),
		PaymentMethod: null.StringFrom("offline"),
		PaymentDate:   null.TimeFrom(time.Now()),
		Quantity:      1,
		Amount:        15.0,
	}
	got = POST_ROOT(t, a, "/v2/order/offline", offline_payment, http.StatusOK)
	fmt.Printf("Offline payment posted: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))

	got = GET(t, a, "/v2/payments", http.StatusOK)
	fmt.Printf("GET payments: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
	payments := got["data"].([]interface{})
	assert.Equal(t, 1, len(payments))
	fmt.Printf("GET payment: %+v\n", payments[0])
	p := payments[0].(map[string]interface{})
	paymentID := int(p["ID"].(float64))
	assert.Equal(t, "success", (p)["PaymentStatus"].(string))

	update := repo.PaymentUpdate{
		PaymentID:   null.IntFrom(paymentID),
		PaymentType: null.StringFrom("offline"),
		Amount:      null.Float64From(float64(16)), // This is actually NOT expected to change!
		Status:      null.StringFrom("some new status"),
	}

	receipt := AttachedFile{Field: "receipt", Filename: "receipt.pdf", Bytes: []byte("fake content")}
	got = PATCH_ROOT_MULTIPART(t, a, "/v2/payment/", update, []AttachedFile{receipt}, http.StatusOK)
	fmt.Printf("Added account: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))

	got = GET(t, a, fmt.Sprintf("/v2/payment/%d", paymentID), http.StatusOK)
	fmt.Printf("Get payment: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
	// Amount is NOT expected to change, we should ignore it's update value.
	assert.Equal(t, float64(15), got["data"].(map[string]interface{})["Amount"].(float64))
	assert.Equal(t, "some new status", got["data"].(map[string]interface{})["PaymentStatus"].(string))
}

// seedOtherAccountPayment writes another user's account, order and payment
// directly: the API only lets a caller create their own account.
func seedOtherAccountPayment(t *testing.T, a *App, email string) {
	t.Helper()
	_, err := a.testPool(t).Exec(context.Background(), `
		WITH acc AS (INSERT INTO accounts ("Email", created_at, updated_at) VALUES ($1, now(), now()) RETURNING id),
		     ord AS (INSERT INTO orders ("AccountID", created_at, updated_at) SELECT id, now(), now() FROM acc RETURNING id)
		INSERT INTO payments ("OrderID", created_at, updated_at) SELECT id, now(), now() FROM ord`, email)
	require.NoError(t, err)
}

// A non-admin is pinned to their own account. Filtering by someone else's
// email must narrow that, not replace it and expose the other account.
func Test_payments_email_filter_stays_within_own_account(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	const otherEmail = "other@example.com"
	seedOtherAccountPayment(t, a, otherEmail)

	got := GET(t, a, "/v2/payments?email="+otherEmail, http.StatusOK)
	assert.Empty(t, got["data"], "non-admin must not see another account's payments")

	got = do(t, a, "GET", "/v2/payments?email="+otherEmail, nil, http.StatusOK, DoOptions{isRoot: true})
	assert.Len(t, got["data"], 1, "admin filtering by email still finds them")
}

// account-id from a non-admin only narrows within their own accounts, so
// another account's id gives an empty list.
func Test_payments_non_admin_foreign_account_id_is_empty(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	seedOtherAccountPayment(t, a, "other@test.test")
	var otherID int
	require.NoError(t, a.testPool(t).QueryRow(context.Background(),
		`SELECT id FROM accounts WHERE "Email" = 'other@test.test'`).Scan(&otherID))

	got := GET(t, a, fmt.Sprintf("/v2/payments?account-id=%d", otherID), http.StatusOK)
	assert.Empty(t, got["data"])
}

// With the email pasted into the SQL, `x') OR TRUE OR ('t` turned the account
// pin into one branch of an OR and returned every account's payments.
func Test_payments_email_injection_stays_within_own_account(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	seedOtherAccountPayment(t, a, "other@example.com")

	got := GET(t, a, "/v2/payments?email="+url.QueryEscape("x') OR TRUE OR ('t"), http.StatusOK)
	assert.Empty(t, got["data"], "an injected email must not widen the non-admin's results")
}

// status sits before the account clause, so the same payload there escaped
// the pin too.
func Test_payments_status_injection_stays_within_own_account(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	seedOtherAccountPayment(t, a, "other@example.com")

	got := GET(t, a, "/v2/payments?status="+url.QueryEscape("x') OR TRUE OR ('t"), http.StatusOK)
	assert.Empty(t, got["data"], "an injected status must not widen the non-admin's results")
}

// Real addresses can contain an apostrophe; pasted into the SQL it was a
// syntax error and a 500.
func Test_payments_email_with_apostrophe_is_found(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	const email = "o'brien@example.com"
	seedOtherAccountPayment(t, a, email)

	got := do(t, a, "GET", "/v2/payments?email="+url.QueryEscape(email), nil, http.StatusOK, DoOptions{isRoot: true})
	assert.Len(t, got["data"], 1)
}

// setCallerEmail sets the caller's account email directly. Through the API
// the owner can set it to anything with PATCH /v2/account/:id, which is why
// it must not be what pins them.
func setCallerEmail(t *testing.T, a *App, email string) {
	t.Helper()
	_, err := a.testPool(t).Exec(context.Background(), `UPDATE accounts SET "Email" = $1 WHERE "UserKey" = $2`, email, USER_KEY)
	require.NoError(t, err)
}

// seedCallerPayment gives the caller's account an order and a payment.
func seedCallerPayment(t *testing.T, a *App) {
	t.Helper()
	_, err := a.testPool(t).Exec(context.Background(), `
		WITH ord AS (INSERT INTO orders ("AccountID", created_at, updated_at)
		             SELECT id, now(), now() FROM accounts WHERE "UserKey" = $1 RETURNING id)
		INSERT INTO payments ("OrderID", created_at, updated_at) SELECT id, now(), now() FROM ord`, USER_KEY)
	require.NoError(t, err)
}

// A non-admin gets their own activities, and only those: a@test.test's
// filter used to be a substring, so it also matched ba@test.test.
func Test_activities_non_admin_sees_only_own_account(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	setCallerEmail(t, a, "a@test.test")
	seedCallerPayment(t, a)
	seedOtherAccountPayment(t, a, "ba@test.test")

	got := GET(t, a, "/payments/activities", http.StatusOK)
	require.Len(t, got["data"], 1)
	assert.Equal(t, "a@test.test", got["data"].([]interface{})[0].(map[string]interface{})["email"])
	assert.Equal(t, float64(1), got["totalCount"])
}

// A user who sets their own email to someone else's must not get that
// person's activities.
func Test_activities_non_admin_with_someone_elses_email_sees_nothing_of_theirs(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	setCallerEmail(t, a, "victim@test.test")
	seedOtherAccountPayment(t, a, "victim@test.test")

	got := GET(t, a, "/payments/activities", http.StatusOK)
	assert.Empty(t, got["data"])
	assert.Equal(t, float64(0), got["totalCount"])
}

// A user can clear their own email (PATCH with "" stores NULL). Pinned by
// email, an absent one dropped the email filter and returned every account's
// activities.
func Test_activities_non_admin_without_email_sees_nothing_of_others(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	_, err := a.testPool(t).Exec(context.Background(), `UPDATE accounts SET "Email" = NULL WHERE "UserKey" = $1`, USER_KEY)
	require.NoError(t, err)
	seedOtherAccountPayment(t, a, "other@test.test")

	got := GET(t, a, "/payments/activities", http.StatusOK)
	assert.Empty(t, got["data"])
	assert.Equal(t, float64(0), got["totalCount"])
}

// An account without an email still belongs to its keycloak id.
func Test_activities_non_admin_sees_payments_on_account_without_email(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	_, err := a.testPool(t).Exec(context.Background(), `UPDATE accounts SET "Email" = NULL WHERE "UserKey" = $1`, USER_KEY)
	require.NoError(t, err)
	seedCallerPayment(t, a)

	got := GET(t, a, "/payments/activities", http.StatusOK)
	assert.Len(t, got["data"], 1)
	assert.Equal(t, float64(1), got["totalCount"])
}

// ?email= from a non-admin narrows within their own account. It used to be
// checked against their email and refused with 403 otherwise; now a matching
// address elsewhere just isn't theirs to see.
func Test_activities_non_admin_email_narrows_within_own_accounts(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	setCallerEmail(t, a, "new@test.test")
	seedCallerPayment(t, a)
	seedOtherAccountPayment(t, a, "xnew@test.test")

	got := GET(t, a, "/payments/activities?email=new@test.test", http.StatusOK)
	require.Len(t, got["data"], 1)
	assert.Equal(t, "new@test.test", got["data"].([]interface{})[0].(map[string]interface{})["email"])
	assert.Equal(t, float64(1), got["totalCount"])
}

// Admins are not pinned and keep the substring search.
func Test_activities_admin_email_is_a_substring_search(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	seedOtherAccountPayment(t, a, "ba@test.test")

	got := do(t, a, "GET", "/payments/activities?email=a@test", nil, http.StatusOK, DoOptions{isRoot: true})
	assert.Len(t, got["data"], 1)
	assert.Equal(t, float64(1), got["totalCount"])
}

// A bad token-exist is the client's mistake: 400, not the 500 the repo's
// ParseBool failure used to surface as.
func Test_payment_fetch_invalid_token_exist_is_400(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/payments?token-exist=maybe", http.StatusBadRequest)
	assert.Equal(t, "Invalid token-exist value! Accepted values are true or false", got["error"])
}

// A bad account-id or order-id must be named in the error, not reported as a
// bad limit.
func Test_payment_fetch_invalid_account_id_names_the_parameter(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/payments?account-id=x", http.StatusBadRequest)
	assert.Equal(t, "Invalid account-id value! Accepted value is INTEGER", got["error"])
}

func Test_payment_fetch_invalid_order_id_names_the_parameter(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/payments?order-id=x", http.StatusBadRequest)
	assert.Equal(t, "Invalid order-id value! Accepted value is INTEGER", got["error"])
}

// With no to-date the upper bound is now, so a payment written a moment ago
// must still be listed. It is created through the API, which stamps
// updated_at with the app's clock, the same clock as the bound.
func Test_payment_fetch_without_to_date_lists_a_fresh_payment(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	postOfflinePayment(t, a)

	got := GET(t, a, "/v2/payments", http.StatusOK)
	assert.Len(t, got["data"], 1)
}

// postOfflinePayment creates an order and a payment for the caller through
// the API.
func postOfflinePayment(t *testing.T, a *App) {
	t.Helper()
	got := POST_ROOT(t, a, "/v2/order/offline", repo.OfflinePaymentRequest{
		KeycloakID:    null.StringFrom(USER_KEY),
		Currency:      null.StringFrom("usd"),
		PaymentMethod: null.StringFrom("offline"),
		PaymentDate:   null.TimeFrom(time.Now()),
		Quantity:      1,
		Amount:        15.0,
	}, http.StatusOK)
	require.Equal(t, true, got["success"])
}

// seedTwoPayments gives the caller two orders with a payment each, through
// the API, and returns the payment and order ids, oldest first.
func seedTwoPayments(t *testing.T, a *App) (payments, orders [2]int) {
	t.Helper()
	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	postOfflinePayment(t, a)
	postOfflinePayment(t, a)

	rows, err := a.testPool(t).Query(context.Background(), `SELECT id, "OrderID" FROM payments ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	i := 0
	for rows.Next() {
		require.Less(t, i, 2)
		require.NoError(t, rows.Scan(&payments[i], &orders[i]))
		i++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 2, i)
	return payments, orders
}

// listedIDs returns the IDs in a list response.
func listedIDs(t *testing.T, got map[string]any) []int {
	t.Helper()
	var ids []int
	for _, item := range got["data"].([]any) {
		ids = append(ids, int(item.(map[string]any)["ID"].(float64)))
	}
	return ids
}

// from-date is parsed in the handler; the parsed value must reach the query.
func Test_payment_fetch_from_date_filters(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	payments, _ := seedTwoPayments(t, a)
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE payments SET updated_at = '2020-01-01T00:00:00Z' WHERE id = $1`, payments[0])
	require.NoError(t, err)

	got := GET(t, a, "/v2/payments?from-date=2025-01-01T00:00:00Z", http.StatusOK)
	assert.Equal(t, []int{payments[1]}, listedIDs(t, got))
}

// token-exist is parsed in the handler; the parsed value must reach the query
// either way.
func Test_payment_fetch_token_exist_filters(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	payments, _ := seedTwoPayments(t, a)
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE payments SET pelecard_token = 'tok' WHERE id = $1`, payments[1])
	require.NoError(t, err)

	got := GET(t, a, "/v2/payments?token-exist=true", http.StatusOK)
	assert.Equal(t, []int{payments[1]}, listedIDs(t, got))

	got = GET(t, a, "/v2/payments?token-exist=false", http.StatusOK)
	assert.Equal(t, []int{payments[0]}, listedIDs(t, got))
}
