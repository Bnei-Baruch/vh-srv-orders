package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func handleUpdateOrders(c *gin.Context) {
	var o Order
	err := c.BindJSON(&o)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err})
		return
	}

	if o.AccountID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing AccountID"})
		return
	}
	var oi Order
	result := DB.First(&oi, o.ID)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error})
		return
	}

	if result.RowsAffected != 1 {
		fmt.Println(result.RowsAffected)
		c.JSON(http.StatusNotFound, gin.H{"error": "Order ID not found"})
		return
	}

	if o.AccountID != oi.AccountID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account ID mismatch"})
		return
	}

	oi.Status = o.Status
	oi.PaymentDate = time.Now()
	oi.Flag = "renewed"

	savedResult := DB.Save(&oi)

	if savedResult.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": savedResult.Error})
		return
	}

	if savedResult.RowsAffected != 1 {
		fmt.Println(savedResult.RowsAffected)
		c.JSON(http.StatusNotFound, gin.H{"error": "not Saved"})
		return
	}

	c.JSON(http.StatusOK, o)

}
