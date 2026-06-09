package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleCreateOrUpdateManualDiscount(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	var req repo.ManualDiscountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := validateManualDiscountReq(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// For new discounts, verify the keycloak_id exists in accounts.
	if req.ID == nil {
		if _, err := o.repo.GetAccountIDByKeycloakID(c.Request.Context(), req.KeycloakID); err != nil {
			if errors.Is(err, common.ErrNoRowsAffected) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "keycloak_id not found"})
			} else {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("repo.GetAccountIDByKeycloakID: %w", err))
			}
			return
		}
	}

	md, err := o.repo.UpsertManualDiscount(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.UpsertManualDiscount: %w", err))
		}
		return
	}

	if req.ID != nil {
		c.JSON(http.StatusOK, gin.H{"message": "Updated!", "data": md, "success": true})
	} else {
		c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": md, "success": true})
	}
}

func (o *OrdersAPI) handleCancelManualDiscount(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	keycloakID := c.Param("keycloak_id")

	if err := o.repo.CancelManualDiscount(c.Request.Context(), keycloakID); err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.CancelManualDiscount: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cancelled!", "success": true})
}

func (o *OrdersAPI) handleGetAllManualDiscounts(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	search := c.Query("search")
	discounts, err := o.repo.GetAllManualDiscounts(c.Request.Context(), search)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAllManualDiscounts: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": discounts, "success": true})
}

func validateManualDiscountReq(req repo.ManualDiscountReq) error {
	if req.Type != "percent" && req.Type != "fixed_price" {
		return fmt.Errorf("type must be 'percent' or 'fixed_price'")
	}
	if !req.Properties.Valid || len(req.Properties.JSON) == 0 {
		return fmt.Errorf("properties is required")
	}

	var p repo.ManualDiscountProperties
	if err := json.Unmarshal(req.Properties.JSON, &p); err != nil {
		return fmt.Errorf("invalid properties JSON: %w", err)
	}

	switch req.Type {
	case "percent":
		if p.DiscountPct == nil {
			return fmt.Errorf("properties.discount_pct is required for type 'percent'")
		}
		if *p.DiscountPct <= 0 || *p.DiscountPct > 100 {
			return fmt.Errorf("properties.discount_pct must be between 0 (exclusive) and 100 (inclusive)")
		}
	case "fixed_price":
		if p.FixedPrice == nil {
			return fmt.Errorf("properties.fixed_price is required for type 'fixed_price'")
		}
		if p.Currency == nil {
			return fmt.Errorf("properties.currency is required for type 'fixed_price'")
		}
		if *p.FixedPrice < 0 {
			return fmt.Errorf("properties.fixed_price must be non-negative")
		}
	}

	if !req.EndDate.After(req.StartDate) {
		return fmt.Errorf("end_date must be after start_date")
	}

	return nil
}
