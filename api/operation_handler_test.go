package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The test helper sends no JSON content type, so c.Bind reads form values;
// they go in the query string.
func operationPath(path string, v url.Values) string { return path + "?" + v.Encode() }

func Test_operation_revert_unknown_pair_is_404(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := do(t, a, "POST", operationPath("/v2/operation/revert",
		url.Values{"new_email": {"nobody@test.test"}, "old_email": {"none@test.test"}}),
		nil, http.StatusNotFound, DoOptions{isRoot: true})
	assert.Equal(t, "operation not found", got["error"])
}

func Test_operation_rename_then_revert(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	_, err := a.testPool(t).Exec(context.Background(),
		`INSERT INTO accounts ("UserKey", "Email", created_at) VALUES ($1, 'o''old@test.test', now())`, USER_KEY)
	require.NoError(t, err)
	// Production has this table; the migrations never created it.
	_, err = a.testPool(t).Exec(context.Background(), `CREATE TABLE IF NOT EXISTS specials_sep2021 (email text)`)
	require.NoError(t, err)

	pair := url.Values{"new_email": {"o'new@test.test"}, "old_email": {"o'old@test.test"}}
	rename := url.Values{"type": {"email_update"}, "new_keycloak_id": {USER_KEY},
		"new_email": {"o'new@test.test"}, "old_email": {"o'old@test.test"}}
	do(t, a, "POST", operationPath("/v2/operation/", rename), nil, http.StatusOK, DoOptions{isRoot: true})
	do(t, a, "POST", operationPath("/v2/operation/revert", pair), nil, http.StatusOK, DoOptions{isRoot: true})
	do(t, a, "POST", operationPath("/v2/operation/revert", pair), nil, http.StatusInternalServerError, DoOptions{isRoot: true})

	var email string
	require.NoError(t, a.testPool(t).QueryRow(context.Background(),
		`SELECT "Email" FROM accounts WHERE "UserKey" = $1`, USER_KEY).Scan(&email))
	assert.Equal(t, "o'old@test.test", email)
}
