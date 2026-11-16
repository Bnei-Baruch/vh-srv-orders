package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user's activities are restricted to their keycloak id. With no account
// under it they have none, and must not see anyone else's. (A user whose
// account has no email is Test_activities_non_admin_without_email_sees_nothing_of_others.)
func TestGetActivities_UserWithoutAccountSeesNothing(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	pool := a.testPool(t)
	ctx := context.Background()

	// Someone else's activity, which must not leak.
	var otherAccount, otherOrder int
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO accounts ("Email", "UserKey") VALUES ('other@test.test', 'kc-other') RETURNING id`).Scan(&otherAccount))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orders ("AccountID") VALUES ($1) RETURNING id`, otherAccount).Scan(&otherOrder))
	_, err := pool.Exec(ctx, `INSERT INTO payments ("OrderID") VALUES ($1)`, otherOrder)
	require.NoError(t, err)

	got := GET(t, a, "/payments/activities", http.StatusOK)
	assert.Empty(t, got["data"])
	assert.EqualValues(t, 0, got["totalCount"])
}
