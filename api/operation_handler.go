package api

import (
	"errors"
	"fmt"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

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
		switch {
		case errors.Is(dbErr, common.ErrAccountKeyTaken):
			c.JSON(http.StatusConflict, gin.H{"error": dbErr.Error()})
		case errors.Is(dbErr, common.ErrInvalidValues):
			c.JSON(http.StatusBadRequest, gin.H{"error": dbErr.Error()})
		default:
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.PerformOperation: %w", dbErr))
		}
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
		switch {
		case errors.Is(revertErr, pgx.ErrNoRows):
			c.JSON(http.StatusNotFound, gin.H{"error": "no operation for these emails"})
		case errors.Is(revertErr, common.ErrAccountKeyTaken):
			c.JSON(http.StatusConflict, gin.H{"error": revertErr.Error()})
		default:
			_ = c.Error(fmt.Errorf("repo.RevertOperation: %w", revertErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": revertErr.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": true, "message": "Reverted!"})
}
