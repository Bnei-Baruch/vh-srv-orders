package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/guregu/null.v4"
)

func handleUpdateOrders(c *gin.Context) {
	var o Order
	err := c.BindJSON(&o)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err})
		return
	}

	if o.AccountID == null.NewInt(0, true) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing AccountID"})
		return
	}
	var oi Order

	if err := DB.QueryRow(c, `SELECT id, "AccountID" from orders WHERE id = $1`, o.ID).Scan(
		&oi.ID, &oi.AccountID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err, "msg": "error finding order"})
		return
	}

	if o.AccountID != oi.AccountID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account ID mismatch"})
		return
	}

	oi.Status = o.Status
	if oi.Status == null.NewString("success", true) || oi.Status == null.NewString("paid", true) {
		oi.PaymentDate = null.NewTime(time.Now(), true)
		oi.Flag = null.NewString("renewed", true)
	}

	updateRes, err := DB.Exec(c, `UPDATE orders 
		SET 
		"Status"=$1,
		"PaymentDate"=$2,
		"Flag"=$3,
		updated_at=$4 
		WHERE id = $5`,
		oi.Status.String, oi.PaymentDate.Time, oi.Flag.String, time.Now(), oi.ID)
	if err != nil {
		fmt.Errorf("problem updating orders: %w", err)
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
