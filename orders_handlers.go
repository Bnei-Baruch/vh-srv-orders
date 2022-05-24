package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

/**
TODO: add update route (POST) taking a JSON
TODO: change price and currency
TODO: Add log of operations
TODO: add field for end
TODO: add route has active order for account - yes / no / data
TODO: update muhlafim
TODO: add invoice
TODO: fix issues in DB
TODO: add payment method
TODO: clean data
**/

//TODO: Rewrite and merge with new & pay
func handleOrdersCreate(c *gin.Context) {
	var req RequestOrder
	err := c.BindJSON(&req)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	ord, err := createOrder(c, req)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, ord)
	}
}

func handleGetOrderByID(ctx *gin.Context) {
	id := ctx.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	uIntId := uint(intID)

	order := getOrderByID(ctx, uIntId)

	if order.ID == 0 {
		// Need to return proper error before this implementation
		/* if err == pgx.ErrNoRows {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		} */
		ctx.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	} else {
		ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": order, "success": true})
		return
	}
}

func handleOrdersPaid(c *gin.Context) {
	var rp RequestPaid
	err := c.BindJSON(&rp)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	if len(rp.UserKey.String) == 0 {
		log.Println("Err: No Order ID provided")
		log.Println(">> ParamX: " + rp.ParamX.String)
		c.JSON(http.StatusBadRequest, gin.H{"Error": "No order id provided in UserKey"})
		return
	}

	p, err := updatePayment(c, rp)

	if err != nil {
		// TODO : ask grisha to return more info on error
		c.JSON(http.StatusUnprocessableEntity, gin.H{"Error": err})
		return
	}

	o, err := updateOrderAfterPayment(c, p)

	if p.PaymentStatus.String == "success" && o.ProductType.String == "jan2022ticket" {
		log.Println("Synch with Registration")
		err := syncServiceRegistration(c, p, o)

		if err != nil {
			log.Println("we have an error")
			log.Println(err)
		}
	}
	c.JSON(http.StatusOK, nil)
}

func handleOrdersRenew(c *gin.Context) {
	type req struct {
		User string `json:"user"`
		Key  string `json:"key"`
	}

	var body req

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error()})
	} else {
		if body.User == "admin" && (body.Key == "t" || body.Key == "e") {
			fmt.Printf("Renewing with key : %s\n", body.Key)
			count := chargeOrdersToRenew(c, body.Key)
			c.JSON(http.StatusOK, gin.H{"count": count})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"Error": "You are not allowed here"})
		}
	}
}

func handleOrderFetch(ctx *gin.Context) {
	skip := ctx.Query("skip")
	limit := ctx.Query("limit")
	fromDate := ctx.Query("from-date")
	toDate := ctx.Query("to-date")
	productType := ctx.Query("product-type")
	currency := ctx.Query("currency")
	status := ctx.Query("status")
	organisation := ctx.Query("org")
	email := ctx.Query("email")
	accountID := ctx.Query("account-id")

	var (
		intAccountID int
		toDateParsed time.Time
		err          error
	)

	if toDate != "" {
		rfcLayout := time.RFC3339
		toDateParsed, err = time.Parse(rfcLayout, toDate)

		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date-from"})
		}
	} else {
		toDateParsed = time.Now()
	}

	if skip == "" {
		skip = "0"
	}

	if limit == "" {
		limit = "10"
	}

	// String conversion to int
	intSkip, err := strconv.Atoi(skip)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

	// String conversion to int
	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
		return
	}

	if accountID != "" {
		intAccountID, err = strconv.Atoi(accountID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
			return
		}
	} else {
		intAccountID = 0
	}

	orders, err := GetAllOrders(ctx, intSkip, intLimit, fromDate, &toDateParsed, productType, currency, status, organisation, email, intAccountID)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": orders, "success": true})
}
