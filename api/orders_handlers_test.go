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

	// Payments looks up the caller's account before it reads the dates.
	POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)

	got := GET(t, a, "/v2/payments?from-date=not-a-date", http.StatusBadRequest)
	assert.Equal(t, "Invalid from-date", got["error"])
}

// victimCard seeds another user's account with an order and a saved card, as
// admin, and returns the account and card ids.
func victimCard(t *testing.T, a *App) (accountID, cardID int) {
	t.Helper()
	got := POST_ROOT(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom("kc-victim")}, http.StatusCreated)
	accountID = int(got["data"].(float64))
	got = POST_ROOT(t, a, "/v2/order/", repo.Order{Amount: null.Float64From(15), AccountID: null.IntFrom(accountID)}, http.StatusCreated)
	orderID := int(got["data"].(float64))
	POST_ROOT(t, a, "/v2/order/update_token", repo.RequestUpdateToken{
		CardExp: "0627", CardNumber: "1234123412341234", OrderId: orderID, Token: "victim-token",
		ParamX: fmt.Sprintf("new_token_%d", orderID),
	}, http.StatusOK)
	require.NoError(t, a.testPool(t).QueryRow(context.Background(),
		`SELECT card_details_id FROM orders WHERE id = $1`, orderID).Scan(&cardID))
	return accountID, cardID
}

// ownOrder creates an account and an order for the non-admin USER_KEY.
func ownOrder(t *testing.T, a *App) (accountID, orderID int) {
	t.Helper()
	got := POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	accountID = int(got["data"].(float64))
	got = POST(t, a, "/v2/order/", repo.Order{Amount: null.Float64From(15), AccountID: null.IntFrom(accountID)}, http.StatusCreated)
	return accountID, int(got["data"].(float64))
}

func orderLinks(t *testing.T, a *App, orderID int) (accountID int, cardID null.Int) {
	t.Helper()
	require.NoError(t, a.testPool(t).QueryRow(context.Background(),
		`SELECT "AccountID", card_details_id FROM orders WHERE id = $1`, orderID).Scan(&accountID, &cardID))
	return accountID, cardID
}

// Monthly billing charges the card an order points at. An owner who could
// point their order at someone else's card would have their membership
// charged to it.
func Test_patch_own_order_onto_someone_elses_card_is_forbidden(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	_, cardID := victimCard(t, a)
	_, orderID := ownOrder(t, a)

	w := sendAsUser(t, a, http.MethodPatch, fmt.Sprintf("/v2/order/%d", orderID), repo.Order{CardDetailsId: null.IntFrom(cardID)})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	_, gotCard := orderLinks(t, a, orderID)
	assert.False(t, gotCard.Valid, "card_details_id was set to %d", gotCard.Int)
}

func Test_patch_own_order_onto_someone_elses_account_is_forbidden(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	victimAccountID, _ := victimCard(t, a)
	accountID, orderID := ownOrder(t, a)

	w := sendAsUser(t, a, http.MethodPatch, fmt.Sprintf("/v2/order/%d", orderID), repo.Order{AccountID: null.IntFrom(victimAccountID)})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	gotAccount, _ := orderLinks(t, a, orderID)
	assert.Equal(t, accountID, gotAccount)
}

func Test_create_order_with_someone_elses_card_is_forbidden(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	_, cardID := victimCard(t, a)
	got := POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	accountID := int(got["data"].(float64))

	w := sendAsUser(t, a, http.MethodPost, "/v2/order/", repo.Order{
		Amount: null.Float64From(15), AccountID: null.IntFrom(accountID), CardDetailsId: null.IntFrom(cardID),
	})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var n int
	require.NoError(t, a.testPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM orders WHERE "AccountID" = $1`, accountID).Scan(&n))
	assert.Zero(t, n)
}

// vh-payment's "update payment" page sends only Amount and Currency.
func Test_owner_may_still_update_amount_and_currency(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	_, orderID := ownOrder(t, a)

	w := sendAsUser(t, a, http.MethodPatch, fmt.Sprintf("/v2/order/%d", orderID),
		repo.Order{Amount: null.Float64From(30), Currency: null.StringFrom("USD")})

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func Test_admin_may_move_order_to_another_account_and_card(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	victimAccountID, cardID := victimCard(t, a)
	_, orderID := ownOrder(t, a)

	PATCH_ROOT(t, a, fmt.Sprintf("/v2/order/%d", orderID),
		repo.Order{AccountID: null.IntFrom(victimAccountID), CardDetailsId: null.IntFrom(cardID)}, http.StatusOK)

	gotAccount, gotCard := orderLinks(t, a, orderID)
	assert.Equal(t, victimAccountID, gotAccount)
	assert.Equal(t, null.IntFrom(cardID), gotCard)
}
