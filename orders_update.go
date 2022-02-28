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

	if err := DB.QueryRow(c, `select id AccountID, PaymentDate, Flag from orders where id = $1`, o.ID).Scan(
		&oi.ID, &oi.AccountID, &oi.PaymentDate, &oi.Flag,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err, "msg": "error finding order"})
	}

	if o.AccountID != oi.AccountID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account ID mismatch"})
		return
	}

	oi.Status = o.Status
	if oi.Status == "success" || oi.Status == "paid" {
		oi.PaymentDate = time.Now()
		oi.Flag = "renewed"
	}

	updateRes, err := DB.Exec(c, `UPDATE payments 
		SET
		Status=$1,
		PaymentDate=$2,
		Flag=$3,
		updated_at=$4 
		WHERE id = $5`,
		oi.Status, oi.PaymentDate, oi.Flag, time.Now(), oi.ID)
	if err != nil {
		fmt.Errorf("problem updating audience: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		fmt.Println(updateRes.RowsAffected())
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not Saved"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, oi)
}
