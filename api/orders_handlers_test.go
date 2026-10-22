package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, "Invalid to-date", got["error"])
}

// Same as above for payments.
func Test_payment_fetch_invalid_to_date_is_rejected(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/payments?to-date=not-a-date", http.StatusBadRequest)
	assert.Equal(t, "Invalid to-date", got["error"])
}

// A bad account-id must be named in the error, not reported as a bad limit.
func Test_order_fetch_invalid_account_id_names_the_parameter(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/orders?account-id=x", http.StatusBadRequest)
	assert.Equal(t, "Invalid account-id value! Accepted value is INTEGER", got["error"])
}

// A bad from-date is the client's mistake: 400, not the 500 the repo's own
// parse failure used to surface as.
func Test_order_fetch_invalid_from_date_is_400(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/orders?from-date=not-a-date", http.StatusBadRequest)
	assert.Equal(t, "Invalid from-date", got["error"])
}

func Test_payment_fetch_invalid_from_date_is_400(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := GET(t, a, "/v2/payments?from-date=not-a-date", http.StatusBadRequest)
	assert.Equal(t, "Invalid from-date", got["error"])
}

// from-date is parsed in the handler; the parsed value must reach the query.
// Listed as root: offline orders don't carry the caller's keycloak id, which
// a non-admin's orders are filtered by.
func Test_order_fetch_from_date_filters(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	_, orders := seedTwoPayments(t, a)
	_, err := a.testPool(t).Exec(context.Background(),
		`UPDATE orders SET updated_at = '2020-01-01T00:00:00Z' WHERE id = $1`, orders[0])
	require.NoError(t, err)

	got := do(t, a, "GET", "/v2/orders?from-date=2025-01-01T00:00:00Z", nil, http.StatusOK, DoOptions{isRoot: true})
	assert.Equal(t, []int{orders[1]}, listedIDs(t, got))
}
