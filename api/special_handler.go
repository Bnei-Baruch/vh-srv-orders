package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleSpecialDeleteByEmail(c *gin.Context) {
	// Mark the record as deleted by setting the end_date to the current time, without actually removing the record from the table
	email := c.Param("email")
	err := o.repo.DeleteSpecialByEmail(c.Request.Context(), email)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.DeleteSpecialByEmail: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleCreateSpecial(c *gin.Context) {
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

	special, err := o.repo.GetSpecialByEmail(c.Request.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetSpecialByEmail: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": special, "success": true})
}

func (o *OrdersAPI) handleSpecialGetById(c *gin.Context) {
	keycloakID := c.Param("id")
	if keycloakID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required", "success": false})
		return
	}

	special, err := o.repo.GetSpecialByKeycloakID(c.Request.Context(), keycloakID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetSpecialByKeycloakID: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": special, "success": true})
}
