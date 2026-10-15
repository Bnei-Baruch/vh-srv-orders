package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func Test_create_and_get_account(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	accountReq := repo.Account{FirstName: null.StringFrom("Test"), UserKey: null.StringFrom(USER_KEY)}
	got := POST(t, a, "/v2/account/", accountReq, http.StatusCreated)
	fmt.Printf("Added account: %+v\n", got)
	accountID := int(got["data"].(float64))

	got = GET(t, a, fmt.Sprintf("/v2/account/%d", accountID), http.StatusOK)
	fmt.Printf("Get order: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
	assert.Equal(t, "Test", got["data"].(map[string]interface{})["FirstName"].(string))
}

// accounts_userkey_uniq allows one account per key; a second create for the
// same key is a conflict, not a server error.
func TestCreateAccount_KeyThatHasAnAccountIsConflict(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	req := repo.Account{FirstName: null.StringFrom("Test"), UserKey: null.StringFrom("kc-create-twice")}
	POST(t, a, "/v2/account/", req, http.StatusCreated)
	POST(t, a, "/v2/account/", req, http.StatusConflict)
}
