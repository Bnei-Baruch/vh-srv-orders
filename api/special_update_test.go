package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// An empty keycloak_id would fail specials_identifiers_not_empty; it is the
// caller's mistake, answered with 400 rather than 500.
func TestSpecialUpdate_EmptyKeycloakIDIsBadRequest(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	POST_ROOT(t, a, "/v2/special/update", gin.H{"keycloak_id": "", "email": "x@test.test"}, http.StatusBadRequest)
}
