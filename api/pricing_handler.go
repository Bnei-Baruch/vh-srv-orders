package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gitlab.bbdev.team/vh/pay/orders/common"
)

func (o *OrdersAPI) handleMonthlyPriceByKCID(c *gin.Context) {
	keycloakId := c.Param("keycloak_id")
	if !o.isSubjectOrHasAnyRole(c, keycloakId, common.RoleRoot, common.RoleAdmin) {
		return
	}

	price, err := o.repo.GetMonthlyPriceByKCID(c.Request.Context(), keycloakId)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.handleMonthlyPriceByKCID: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": price, "success": true})
}
