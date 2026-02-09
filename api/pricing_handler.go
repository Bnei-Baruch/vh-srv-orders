package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"gitlab.bbdev.team/vh/pay/orders/common"
)

func (o *OrdersAPI) handleMonthlyPriceByKCID(c *gin.Context) {
	keycloakId := c.Param("keycloak_id")
	if !o.isSubjectOrHasAnyRole(c, keycloakId, common.RoleRoot, common.RoleAdmin) {
		return
	}

	// Get user's preferred currency from query parameter (optional)
	// Example: /pay/v2/pricing/monthly/{keycloak_id}?currency=nis
	preferredCurrency := c.Query("currency")

	price, err := o.repo.GetMonthlyPriceByKCID(c.Request.Context(), keycloakId, preferredCurrency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The given KeycloakID is not found.", "success": false})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.handleMonthlyPriceByKCID: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": price, "success": true})
}
