package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

func createSpecials(t *testing.T, a *App, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		req := repo.Special{
			KeycloakId: null.StringFrom(fmt.Sprintf("kc-list-%d", i)),
			Email:      null.StringFrom(fmt.Sprintf("list%d@example.com", i)),
			StartDate:  null.TimeFrom(time.Now()),
			EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
			Category:   null.StringFrom("membership"),
		}
		POST_ROOT(t, a, "/v2/special/", req, http.StatusCreated)
	}
}

func TestSpecialGetAll_TotalDistinguishesAFullPageFromTheWholeTable(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	createSpecials(t, a, 3)

	got := GET_ROOT(t, a, "/v2/special/?skip=0&limit=2", http.StatusOK)
	assert.Len(t, got["data"].([]interface{}), 2, "the page is bounded by limit")
	assert.Equal(t, float64(3), got["total"], "and total says how many there are")
	assert.Equal(t, float64(2), got["limit"])
	assert.Equal(t, float64(0), got["skip"])
}

// seedSpecials inserts n rows in one statement; the POST route is too slow
// for a table bigger than the page maximum.
func seedSpecials(t *testing.T, a *App, n int) {
	t.Helper()
	_, err := a.testPool(t).Exec(t.Context(), `INSERT INTO specials (email, start_date, end_date, category, created_at)
		SELECT 'seed' || i || '@example.com', now(), now() + interval '1 day', 'membership', now()
		FROM generate_series(1, $1) AS i`, n)
	require.NoError(t, err)
}

// vh-payment-bo asks for no page and has no pagination of its own, so any
// default cap would silently cut its table short.
func TestSpecialGetAll_NoLimitReturnsTheWholeTable(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	seedSpecials(t, a, 5001)

	got := GET_ROOT(t, a, "/v2/special/", http.StatusOK)
	assert.Len(t, got["data"].([]interface{}), 5001, "more than the page maximum")
	assert.Equal(t, float64(5001), got["total"])
	assert.Nil(t, got["limit"], "no page was applied")
}

func TestSpecialGetAll_LimitIsClampedTo5000(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	seedSpecials(t, a, 5001)

	got := GET_ROOT(t, a, "/v2/special/?limit=99999", http.StatusOK)
	assert.Len(t, got["data"].([]interface{}), 5000)
	assert.Equal(t, float64(5000), got["limit"])
	assert.Equal(t, float64(5001), got["total"])
}

// Postgres rejects a negative LIMIT or OFFSET, which would be a 500. limit=0
// is refused too: leaving limit out is how a client asks for every row.
func TestSpecialGetAll_NegativePagingIsABadRequest(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	for _, path := range []string{"/v2/special/?limit=-1", "/v2/special/?skip=-5", "/v2/special/?limit=0"} {
		got := GET_ROOT(t, a, path, http.StatusBadRequest)
		assert.Equal(t, false, got["success"], path)
	}
}

func TestSpecialGetAll_RejectsNonIntegerPaging(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	require.NotNil(t, GET_ROOT(t, a, "/v2/special/?limit=abc", http.StatusBadRequest))
	require.NotNil(t, GET_ROOT(t, a, "/v2/special/?skip=abc", http.StatusBadRequest))
}
