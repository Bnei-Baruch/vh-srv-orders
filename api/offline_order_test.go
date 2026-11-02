package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// An empty KeycloakID is a missing one: the request is refused with 400, not
// passed to the account lookup, which refuses an empty key.
func TestCreateOffline_EmptyKeycloakIDIsBadRequest(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := POST_ROOT(t, a, "/v2/order/offline", gin.H{
		"keycloak_id":    "",
		"currency":       "ILS",
		"amount":         100,
		"payment_date":   "2026-10-01T00:00:00Z",
		"payment_method": "cash",
		"quantity":       1,
	}, http.StatusBadRequest)
	assert.Equal(t, "Missing KeycloakID.", got["error"])
}
