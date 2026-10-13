package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func Test_get_order_by_id(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	accountReq := repo.Account{UserKey: null.StringFrom(USER_KEY)}
	got := POST(t, a, "/v2/account/", accountReq, http.StatusCreated)
	fmt.Printf("Added account: %+v\n", got)
	accountID := int(got["data"].(float64))

	orderReq := repo.Order{Amount: null.Float64From(15), AccountID: null.IntFrom(accountID)}
	got = POST(t, a, "/v2/order/", orderReq, http.StatusCreated)
	fmt.Printf("Added order: %+v\n", got)
	orderID := int(got["data"].(float64))

	got = GET(t, a, fmt.Sprintf("/v2/order/%d", orderID), http.StatusOK)
	fmt.Printf("Get order: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
	assert.Equal(t, float64(15), got["data"].(map[string]interface{})["Amount"].(float64))
}

func Test_duplicate_card_key(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	accountReq := repo.Account{UserKey: null.StringFrom(USER_KEY)}
	got := POST(t, a, "/v2/account/", accountReq, http.StatusCreated)
	fmt.Printf("Added account: %+v\n", got)
	accountID := int(got["data"].(float64))

	orderReq := repo.Order{Amount: null.Float64From(15), AccountID: null.IntFrom(accountID)}
	got = POST(t, a, "/v2/order/", orderReq, http.StatusCreated)
	fmt.Printf("Added order: %+v\n", got)
	orderID := int(got["data"].(float64))

	data := repo.RequestUpdateToken{
		CardExp:    "0627",
		CardNumber: "1234123412341234",
		OrderId:    orderID,
		ParamX:     fmt.Sprintf("new_token_%d", orderID),
		Token:      "1234Token",
	}
	fmt.Printf("Data: %+v\n", data)
	got = POST(t, a, "/v2/order/update_token", data, http.StatusOK)
	fmt.Printf("Update token: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))

	// Secod update token with same card.
	data = repo.RequestUpdateToken{
		CardExp:    "0627",
		CardNumber: "1234123412341234",
		OrderId:    orderID,
		ParamX:     fmt.Sprintf("new_token_%d", orderID),
		Token:      "4321Token",
	}
	fmt.Printf("Data: %+v\n", data)
	got = POST(t, a, "/v2/order/update_token", data, http.StatusOK)
	fmt.Printf("Update token: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
}

// A bad to-date must stop the request at the 400. Before the fix the handler
// fell through, ran the query with a zero date and wrote a second JSON body
// after the first, which GET's json.Unmarshal rejects.
func Test_order_fetch_invalid_to_date_is_rejected(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/orders?to-date=not-a-date", http.StatusBadRequest)
	assert.Equal(t, "Invalid date-from", got["error"])
}

// Same as above for payments, which first looks up the caller's account, so
// the account must exist for the request to reach the date parsing.
func Test_payment_fetch_invalid_to_date_is_rejected(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)

	got := GET(t, a, "/v2/payments?to-date=not-a-date", http.StatusBadRequest)
	assert.Equal(t, "Invalid date-from", got["error"])
}

// A bad account-id or order-id must be named in the error, not reported as a
// bad limit.
func Test_fetch_invalid_id_names_the_parameter(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)

	for path, want := range map[string]string{
		"/v2/orders?account-id=x":   "Invalid account-id value! Accepted value is INTEGER",
		"/v2/payments?account-id=x": "Invalid account-id value! Accepted value is INTEGER",
		"/v2/payments?order-id=x":   "Invalid order-id value! Accepted value is INTEGER",
	} {
		t.Run(path, func(t *testing.T) {
			got := GET(t, a, path, http.StatusBadRequest)
			assert.Equal(t, want, got["error"])
		})
	}
}
