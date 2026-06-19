package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleCreateHHRequest(c *gin.Context) {
	var req repo.HHRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Members submit their own requests; admins may submit on behalf of a member.
	if !o.isSubjectOrHasAnyRole(c, req.KeycloakID, common.RoleRoot, common.RoleAdmin, common.RoleHelpHaverAdmin) {
		return
	}

	if !slices.Contains(common.HHGrantTypes, req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("type must be one of: %v", common.HHGrantTypes)})
		return
	}
	if req.RequestedPct <= 0 || req.RequestedPct > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "requested_pct must be between 0 (exclusive) and 100 (inclusive)"})
		return
	}
	if req.Months <= 0 || req.Months > 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "months must be between 1 and 12"})
		return
	}

	request, err := o.repo.CreateHHRequest(c.Request.Context(), req)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CreateHHRequest: %w", err))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": request, "success": true})
}

func (o *OrdersAPI) handleGetAllHHRequests(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin, common.RoleHelpHaverAdmin) {
		return
	}

	requests, err := o.repo.GetAllHHRequests(c.Request.Context(), c.Query("status"), c.Query("keycloak_id"))
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAllHHRequests: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": requests, "success": true})
}

func (o *OrdersAPI) handleConcludeHHRequest(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin, common.RoleHelpHaverAdmin) {
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	var conclusion repo.HHRequestConclusion
	if err := c.ShouldBindJSON(&conclusion); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if conclusion.Approved {
		if !slices.Contains(common.HHGrantTypes, conclusion.Type) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("type must be one of: %v", common.HHGrantTypes)})
			return
		}
		if conclusion.DiscountPct <= 0 || conclusion.DiscountPct > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "discount_pct must be between 0 (exclusive) and 100 (inclusive)"})
			return
		}
		if conclusion.Months <= 0 || conclusion.Months > 12 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "months must be between 1 and 12"})
			return
		}
	}

	request, err := o.repo.ConcludeHHRequest(c.Request.Context(), id, conclusion)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.ConcludeHHRequest: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Concluded!", "data": request, "success": true})
}
