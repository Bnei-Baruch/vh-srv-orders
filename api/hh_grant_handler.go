package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Grants are created only by approving a request (see handleConcludeHHRequest);
// this handler only ends an active grant early.
func (o *OrdersAPI) handleCancelHHGrant(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin, common.RoleHelpHaverAdmin) {
		return
	}

	keycloakID := c.Param("keycloak_id")

	if err := o.repo.CancelHHGrant(c.Request.Context(), keycloakID); err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.CancelHHGrant: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cancelled!", "success": true})
}
