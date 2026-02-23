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

	// Get pricing version from query parameter (optional, defaults to v1)
	// Supported versions:
	//   v1: Static pricing (legacy frontend pricing)
	//   v2: Country-based tiered pricing
	//   t1: Tier 1 rollout (IL/NIS scope uses v2, others use v1)
	// Example: /pay/v2/pricing/monthly/{keycloak_id}?pricing_version=t1
	pricingVersion := c.Query("pricing_version")

	price, err := o.repo.GetMonthlyPriceByKCID(c.Request.Context(), keycloakId, preferredCurrency, pricingVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The given KeycloakID is not found.", "success": false})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetMonthlyPriceByKCID: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": price, "success": true})
}
