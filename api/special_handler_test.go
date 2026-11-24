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

func TestSpecialGetAll_LimitIsCappedNotJustDefaulted(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	createSpecials(t, a, 1)

	got := GET_ROOT(t, a, fmt.Sprintf("/v2/special/?limit=%d", maxSpecialsPageSize*1000), http.StatusOK)
	assert.Equal(t, float64(maxSpecialsPageSize), got["limit"], "an oversized request is clamped to the maximum")
}

// Postgres rejects a negative LIMIT or OFFSET, which would be a 500.
func TestSpecialGetAll_NegativePagingIsABadRequest(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	for _, path := range []string{"/v2/special/?limit=-1", "/v2/special/?skip=-5"} {
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
