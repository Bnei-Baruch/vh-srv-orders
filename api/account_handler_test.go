package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
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

// sendAsUser sends a request as the non-admin USER_KEY and returns the raw
// recorder: a refusal has no body for do() to decode.
func sendAsUser(a *App, method, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	claims := &middleware.IDTokenClaims{Sub: USER_KEY, RealmAccess: middleware.Roles{Roles: []string{"some-role"}}}
	w := httptest.NewRecorder()
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	return w
}

func countAccountsWithUserKey(t *testing.T, a *App, key string) int {
	t.Helper()
	var n int
	require.NoError(t, a.testPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM accounts WHERE "UserKey" = $1`, key).Scan(&n))
	return n
}

// New orders go to the newest account with the buyer's keycloak id. A user
// who could create an account under someone else's id would have that
// person's next order land on an account with the user's own email and name.
func Test_create_account_with_someone_elses_userkey_is_forbidden(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	w := sendAsUser(a, http.MethodPost, "/v2/account/", repo.Account{UserKey: null.StringFrom("kc-victim")})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Zero(t, countAccountsWithUserKey(t, a, "kc-victim"))
}

// The same through PATCH on the caller's own account.
func Test_patch_own_account_to_someone_elses_userkey_is_forbidden(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := POST(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom(USER_KEY)}, http.StatusCreated)
	id := int(got["data"].(float64))

	w := sendAsUser(a, http.MethodPatch, fmt.Sprintf("/v2/account/%d", id), repo.Account{UserKey: null.StringFrom("kc-victim")})

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Zero(t, countAccountsWithUserKey(t, a, "kc-victim"))
	assert.Equal(t, 1, countAccountsWithUserKey(t, a, USER_KEY))
}

// Admins create and fix accounts for other users, so any key stays allowed.
func Test_admin_may_create_account_with_any_userkey(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST_ROOT(t, a, "/v2/account/", repo.Account{UserKey: null.StringFrom("kc-someone")}, http.StatusCreated)

	assert.Equal(t, 1, countAccountsWithUserKey(t, a, "kc-someone"))
}
