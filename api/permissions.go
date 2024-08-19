package api

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"net/http"
)

func (o *OrdersAPI) HasAnyRole(c *gin.Context, roles ...string) bool {

	authData := c.Request.Context().Value(common.CtxAuthClaims)
	if authData == nil {
		c.Status(http.StatusForbidden)
		return false
	}
	claims := authData.(*middleware.IDTokenClaims)

	if !claims.HasAnyRole(roles...) {
		c.Status(http.StatusForbidden)
		return false
	}

	return true
}

func (o *OrdersAPI) isSubjectOrHasAnyRole(c *gin.Context, keycloakID string, roles ...string) bool {

	authData := c.Request.Context().Value(common.CtxAuthClaims)
	if authData == nil {
		c.Status(http.StatusForbidden)
		return false
	}
	claims := authData.(*middleware.IDTokenClaims)

	if claims.Sub != keycloakID && !claims.HasAnyRole(roles...) {
		c.Status(http.StatusForbidden)
		return false
	}

	return true
}

func (o *OrdersAPI) isEmailOwnerOrHasAnyRole(c *gin.Context, email string, roles ...string) bool {

	authData := c.Request.Context().Value(common.CtxAuthClaims)
	if authData == nil {
		c.Status(http.StatusForbidden)
		return false
	}
	claims := authData.(*middleware.IDTokenClaims)

	if claims.Email != email && !claims.HasAnyRole(roles...) {
		c.Status(http.StatusForbidden)
		return false
	}

	return true
}

func (o *OrdersAPI) isUserOrHasAnyRole(c *gin.Context, userID string, roles ...string) bool {
	authData := c.Request.Context().Value(common.CtxAuthClaims)
	if authData == nil {
		c.Status(http.StatusForbidden)
		return false
	}
	claims := authData.(*middleware.IDTokenClaims)

	if claims.HasAnyRole(roles...) {
		return true
	}

	match, err := o.repo.IsSubjectID(c.Request.Context(), claims.Sub, userID)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.IsSubjectID: %w", err))
		return false
	}

	if !match {
		c.Status(http.StatusForbidden)
		return false
	}

	return true
}

func (o *OrdersAPI) getUserKeyFromRequest(c *gin.Context) (string, bool) {

	authData := c.Request.Context().Value(common.CtxAuthClaims)
	if authData == nil {
		c.Status(http.StatusForbidden)
		return "", false
	}
	claims := authData.(*middleware.IDTokenClaims)
	if claims == nil {
		c.Status(http.StatusForbidden)
		return "", false
	}

	return claims.Sub, true
}

// isAuthUserOrHasAnyRole checks whether the user is authenticated and/or has any of the specified roles.
// It accepts a gin context and a set of roles as parameters.
// The method returns three values:
// 1) bool - signaling whether the user's Keycloak ID was successfully retrieved from the request.
// 2) bool - indicating whether the user is an administrator (has any of the specific roles).
// 3) string - the Keycloak ID of the user.
// If the user cannot be identified (i.e., their Keycloak ID cannot be retrieved from the request),
// the method sets the http response status to `StatusForbidden` (403).
func (o *OrdersAPI) isAuthUserOrHasAnyRole(c *gin.Context, roles ...string) (bool, bool, string) {
	var (
		keycloakID string
		ok         bool
		isAdmin    bool
	)
	keycloakID, ok = o.getUserKeyFromRequest(c)
	if ok {
		isAdmin = o.HasAnyRole(c, roles...)
	} else {
		c.Status(http.StatusForbidden)
	}
	return ok, isAdmin, keycloakID
}
