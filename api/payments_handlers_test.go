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
