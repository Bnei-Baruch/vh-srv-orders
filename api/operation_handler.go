package api

import (
	"fmt"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleOperationCreate(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	var opr repo.OperationReq

	if err := c.Bind(&opr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if opr.Type == nil || *opr.Type != "email_update" ||
		opr.NewEmail == nil || opr.NewKeycloakID == nil {
		if opr.Type == nil || *opr.Type != "email_update" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "type should be email_update"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "new email or new keycloak id missing"})
		}
		return
	}

	ID, dbErr := o.repo.PerformOperation(c.Request.Context(), opr)

	if dbErr != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("error while creating grant: %w", dbErr))
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": true, "message": "Created!", "data": ID})
}

func (o *OrdersAPI) handleOperationRevert(c *gin.Context) {

	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	var opr repo.OperationReq

	if err := c.Bind(&opr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if opr.NewEmail == nil || opr.OldEmail == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new email and old email missing"})
		return
	}

	revertErr := o.repo.RevertOperation(c.Request.Context(), *opr.NewEmail, *opr.OldEmail)

	if revertErr != nil {
		_ = c.Error(fmt.Errorf("error while reverting operation: %w", revertErr))
		c.JSON(http.StatusInternalServerError, gin.H{"error": revertErr.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": true, "message": "Reverted!"})
}
