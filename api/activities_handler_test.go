package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user's own activities are filtered by their account's email. With no
// account, or an account with no email, there are none; the request must
// neither fail scanning a NULL email nor run with an empty email filter, which
// would list everyone's activities.
func TestGetActivities_UserWithoutEmailSeesNothing(t *testing.T) {
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

	t.Run("no account", func(t *testing.T) {
		got := GET(t, a, "/payments/activities", http.StatusOK)
		assert.Empty(t, got["data"])
		assert.EqualValues(t, 0, got["totalCount"])
	})

	_, err = pool.Exec(ctx, `INSERT INTO accounts ("UserKey") VALUES ($1)`, USER_KEY)
	require.NoError(t, err)

	t.Run("account without email", func(t *testing.T) {
		got := GET(t, a, "/payments/activities", http.StatusOK)
		assert.Empty(t, got["data"])
		assert.EqualValues(t, 0, got["totalCount"])
	})
}
