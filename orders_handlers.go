package main

import (
	"errors"
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

// TODO: Rewrite and merge with new & pay
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

func handleV2OrderCreate(ctx *gin.Context) {
	var req Order
	errRequest := ctx.BindJSON(&req)

	if errRequest != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	if req.AccountID.Int64 == 0 {
		ctx.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing AccountID"})
		return
	}

	orderID, err := createV2Order(ctx, req)

	if err != nil {
		if errors.Is(err, errInvalidBody) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": orderID, "success": true})
}

func handleOrderDeleteByID(ctx *gin.Context) {
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

	err = softDeleteOrderByID(ctx, intID)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func handleOrderUpdateByID(ctx *gin.Context) {
	var req Order
	errRequest := ctx.BindJSON(&req)

	id := ctx.Param("id")

	if errRequest != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = patchOrderByID(ctx, req, intID)

	if err != nil {
		fmt.Printf("Error while updating the order: %s\n", err)
		fmt.Printf("Order body: %+v\n", req)
		if errors.Is(err, errInvalidBody) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
}

func handleOrderGetByID(ctx *gin.Context) {
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
	evaluateMembership := ctx.Query("evaluate-membership")
	accountID := ctx.Query("account-id")
	orderByPaymentDate := ctx.Query("o-payment-date")
	if orderByPaymentDate != "" && orderByPaymentDate != "desc" && orderByPaymentDate != "asc" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid o-created-at value! Accepted values are desc for descending & asc for ascending"})
		return
	}

	if evaluateMembership != "" && evaluateMembership != "true" && evaluateMembership != "false" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid active-membership value! Accepted values are true or false"})
		return
	}

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

	orders, err := GetAllOrders(ctx, intSkip, intLimit, fromDate, &toDateParsed, productType, currency, status, organisation, email, intAccountID, evaluateMembership, orderByPaymentDate)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": orders, "success": true})
}
