package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

func (o *OrdersAPI) handleSpecialHardDeleteByEmail(c *gin.Context) {
	email := c.Param("email")

	err, rowsAffected := o.repo.HardDeleteSpecialByEmail(c, email)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true, "data": rowsAffected})
}

func (o *OrdersAPI) handleSpecialGetByEmail(c *gin.Context) {
	email := c.Param("email")

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required", "success": false})
		return
	}

	special, err := o.repo.GetSpecialByEmail(c, email)

	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Special not found"})
			return
		}
		fmt.Println("Error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": special, "success": true})
		return
	}
}
