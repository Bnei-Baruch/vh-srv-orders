package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func handleCreatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)
	fmt.Println(p)
	if p.OrderID == 0 {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing OrderID"})
		return
	}

	fmt.Println(p)
	result := DB.Create(&p)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error})
		return
	}

	if result.RowsAffected != 1 {
		fmt.Println(result.RowsAffected)
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not created"})
		return
	}

	c.JSON(http.StatusOK, p)
}

func handleUpdatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)

	if p.OrderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing OrderID"})
		return
	}
	var pi Payment
	result := DB.First(&pi, p.ID)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error})
		return
	}

	if result.RowsAffected != 1 {
		fmt.Println(result.RowsAffected)
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment ID not found"})
		return
	}

	if p.OrderID != pi.OrderID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order ID mismatch"})
		return
	}

	savedResult := DB.Save(&p)

	if savedResult.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": savedResult.Error})
		return
	}

	if savedResult.RowsAffected != 1 {
		fmt.Println(savedResult.RowsAffected)
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not Saved"})
		return
	}

	c.JSON(http.StatusOK, p)

}
