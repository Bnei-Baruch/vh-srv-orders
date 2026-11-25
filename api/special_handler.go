package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

func (o *OrdersAPI) handleSpecialDeleteById(c *gin.Context) {
	// Mark the record as deleted by setting the end_date to the current time, without actually removing the record from the table
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	err = o.repo.DeleteSpecialById(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.DeleteSpecialById: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleDeleteSpecialsByKeycloakId(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	keycloakId := c.Param("keycloak_id")
	err := o.repo.DeleteSpecialsByKeycloakId(c.Request.Context(), keycloakId)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.handleDeleteSpecialsByKeycloakId: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleSpecialUpdateKeycloakIdByEmail(c *gin.Context) {
	var (
		req repo.SpecialKeycloakIdUpdate
		err error
	)
	if err = c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !o.isEmailOwnerAndSubjectOrHasAnyRole(c, req.KeycloakId, req.Email, common.RoleRoot, common.RoleAdmin) {
		return
	}
	err = o.repo.SetKeycloakIdByEmail(c.Request.Context(), req.Email, req.KeycloakId)
	if err != nil {
		if errors.Is(err, common.ErrInvalidValues) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.SetKeycloakIdByEmail: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "KeycloakId Updated!", "success": true})
}

func (o *OrdersAPI) handleCreateSpecial(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	var req repo.Special
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	specialId, err := o.repo.CreateSpecial(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, common.ErrInvalidValues) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.CreateSpecial: %w", err))
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": specialId, "success": true})
}

func (o *OrdersAPI) handleSpecialGetByEmail(c *gin.Context) {
	email := c.Param("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required", "success": false})
		return
	}

	if !o.isEmailOwnerOrHasAnyRole(c, email, common.RoleRoot, common.RoleAdmin) {
		return
	}

	specials, err := o.repo.GetAllSpecialsByEmail(c.Request.Context(), email)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAllSpecialsByEmail: %w", err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": specials, "success": true})
}

func (o *OrdersAPI) handleSpecialGetById(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required", "success": false})
		return
	}
	special, err := o.repo.GetSpecialsById(c.Request.Context(), id)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetSpecialsById: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": special, "success": true})
}

func (o *OrdersAPI) handleSpecialGetByKeycloakId(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	keycloakID := c.Param("keycloak_id")
	if keycloakID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keycloakID is required", "success": false})
		return
	}

	special, err := o.repo.GetSpecialsByKeycloakId(c.Request.Context(), keycloakID)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetSpecialsByKeycloakId: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": special, "success": true})
}

// vh-payment-bo, the only consumer, asks for no page and shows every row, so
// leaving limit out still returns the whole table; paging is opt-in. A given
// limit must be positive and is clamped to the maximum.
const maxSpecialsPageSize = 5000

func (o *OrdersAPI) handleSpecialGetAll(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	skip := c.Query("skip")
	if skip == "" {
		skip = "0"
	}
	intSkip, err := strconv.Atoi(skip)
	if err != nil || intSkip < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is a non-negative INTEGER", "success": false})
		return
	}

	// 0 asks GetAllSpecials for every row.
	intLimit := 0
	if limit := c.Query("limit"); limit != "" {
		intLimit, err = strconv.Atoi(limit)
		if err != nil || intLimit < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is a positive INTEGER", "success": false})
			return
		}
		intLimit = min(intLimit, maxSpecialsPageSize)
	}

	special, err := o.repo.GetAllSpecials(c.Request.Context(), intSkip, intLimit)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAllSpecials: %w", err))
		return
	}

	// total tells a full page from the whole table; data is unchanged for
	// existing consumers.
	total, err := o.repo.CountSpecials(c.Request.Context())
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CountSpecials: %w", err))
		return
	}

	var pageLimit any
	if intLimit > 0 {
		pageLimit = intLimit
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": special, "total": total,
		"skip": intSkip, "limit": pageLimit, "success": true})
}
