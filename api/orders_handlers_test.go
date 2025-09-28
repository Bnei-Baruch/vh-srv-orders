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
