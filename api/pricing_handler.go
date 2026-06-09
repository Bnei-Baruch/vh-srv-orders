package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

func (o *OrdersAPI) handleMonthlyPriceByKCID(c *gin.Context) {
	keycloakId := c.Param("keycloak_id")
	if !o.isSubjectOrHasAnyRole(c, keycloakId, common.RoleRoot, common.RoleAdmin) {
		return
	}

	preferredCurrency := strings.ToUpper(c.Query("currency"))
	pricingVersion := c.Query("pricing_version")

	utils.LogFor(c.Request.Context()).Info("handleMonthlyPriceByKCID",
		slog.String("keycloak_id", keycloakId),
		slog.String("pricing_version", pricingVersion),
		slog.String("currency", preferredCurrency),
	)

	accountID, err := o.repo.GetAccountIDByKeycloakID(c.Request.Context(), keycloakId)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The given KeycloakID is not found.", "success": false})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetAccountIDByKeycloakID: %w", err))
		}
		return
	}

	account, err := o.repo.GetAccount(c.Request.Context(), accountID, "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAccount: %w", err))
		return
	}

	price, err := pricing.GetMonthlyPrice(
		c.Request.Context(),
		o.profileService, o.priorityClient, o.accountingService, o.quickbooksCompanyID,
		account.ID, account.UserKey.String, account.Email.String, account.Country.String,
		preferredCurrency, pricingVersion, o.repo,
	)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("pricing.GetMonthlyPrice: %w", err))
		return
	}

	isAdmin := o.HasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": toMonthlyPriceResponse(price, isAdmin), "success": true})
}
