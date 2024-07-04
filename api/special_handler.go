package api

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"net/http"
)

func (o *OrdersAPI) handleSpecialHardDeleteByEmail(c *gin.Context) {

	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	email := c.Param("email")

	err := o.repo.HardDeleteSpecialByEmail(c.Request.Context(), email)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.HardDeleteSpecialByEmail: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}
func (o *OrdersAPI) handleSpecialGetByEmail(c *gin.Context) {

	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

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
