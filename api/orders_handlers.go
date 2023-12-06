package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
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
func (o *OrdersAPI) handleOrdersCreate(c *gin.Context) {
	var req repo.RequestOrder
	err := c.BindJSON(&req)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	ord, err := o.repo.CreateOrder(c, req)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, ord)
	}
}

func (o *OrdersAPI) handleUpdateOrders(c *gin.Context) {
	var order repo.Order
	err := c.BindJSON(&order)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err})
		return
	}

	if order.AccountID == null.NewInt(0, true) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing AccountID"})
		return
	}

	// TODO (edo): this is ugly. repo.GetOrderByID should return some error not found
	oi := o.repo.GetOrderByID(c, uint(order.ID))
	if !oi.CreatedAt.Valid {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err, "msg": "error finding order"})
		return
	}

	if order.AccountID != oi.AccountID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account ID mismatch"})
		return
	}

	oi.Status = order.Status
	if oi.Status == null.NewString("success", true) || oi.Status == null.NewString("paid", true) {
		oi.PaymentDate = null.NewTime(time.Now(), true)
		oi.Flag = null.NewString("renewed", true)
	}

	err = o.repo.PatchOrderByID(c, oi, order.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Errorf("problem updating orders: %w", err)})
		return
	}

	c.JSON(http.StatusOK, oi)
}

func (o *OrdersAPI) handleV2OrderCreate(c *gin.Context) {
	var req repo.Order
	errRequest := c.BindJSON(&req)

	if errRequest != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	if req.AccountID.IsZero() {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing AccountID"})
		return
	}

	orderID, err := o.repo.CreateV2Order(c, req)

	if err != nil {
		if errors.Is(err, common.ErrInvalidBody) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": orderID, "success": true})
}

func (o *OrdersAPI) handleOrderDeleteByID(c *gin.Context) {
	id := c.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = o.repo.SoftDeleteOrderByID(c, intID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleOrderUpdateByID(c *gin.Context) {
	var req repo.Order
	errRequest := c.BindJSON(&req)

	id := c.Param("id")

	if errRequest != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = o.repo.PatchOrderByID(c, req, intID)

	if err != nil {
		fmt.Printf("Error while updating the order: %s\n", err)
		fmt.Printf("Order body: %+v\n", req)
		if errors.Is(err, common.ErrInvalidBody) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
}

func (o *OrdersAPI) handleOrderGetByID(c *gin.Context) {
	id := c.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	uIntId := uint(intID)

	order := o.repo.GetOrderByID(c, uIntId)

	if order.ID == 0 {
		// Need to return proper error before this implementation
		/* if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		} */
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": order, "success": true})
		return
	}
}

func (o *OrdersAPI) handleOrdersPaid(c *gin.Context) {
	var rp repo.RequestPaid
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

	p, err := o.repo.UpdatePayment(c, rp)

	if err != nil {
		// TODO : ask grisha to return more info on error
		c.JSON(http.StatusUnprocessableEntity, gin.H{"Error": err})
		return
	}

	order, err := o.repo.UpdateOrderAfterPayment(c, p)

	if p.PaymentStatus.String == "success" && order.ProductType.String == "jan2022ticket" {
		log.Println("Synch with Registration")
		err := o.repo.SyncServiceRegistration(c, p, order)

		if err != nil {
			log.Println("we have an error")
			log.Println(err)
		}
	}
	c.JSON(http.StatusOK, nil)
}

func (o *OrdersAPI) handleOrdersRenew(c *gin.Context) {
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
			count := o.repo.ChargeOrdersToRenew(c, body.Key)
			c.JSON(http.StatusOK, gin.H{"count": count})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"Error": "You are not allowed here"})
		}
	}
}

func (o *OrdersAPI) handleOrderFetch(c *gin.Context) {
	skip := c.Query("skip")
	limit := c.Query("limit")
	fromDate := c.Query("from-date")
	toDate := c.Query("to-date")
	productType := c.Query("product-type")
	currency := c.Query("currency")
	status := c.Query("status")
	organisation := c.Query("org")
	email := c.Query("email")
	evaluateMembership := c.Query("evaluate-membership")
	accountID := c.Query("account-id")
	orderByPaymentDate := c.Query("o-payment-date")
	if orderByPaymentDate != "" && orderByPaymentDate != "desc" && orderByPaymentDate != "asc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid o-created-at value! Accepted values are desc for descending & asc for ascending"})
		return
	}

	if evaluateMembership != "" && evaluateMembership != "true" && evaluateMembership != "false" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid active-membership value! Accepted values are true or false"})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date-from"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

	// String conversion to int
	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
		return
	}

	if accountID != "" {
		intAccountID, err = strconv.Atoi(accountID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
			return
		}
	} else {
		intAccountID = 0
	}

	orders, err := o.repo.GetAllOrders(c, intSkip, intLimit, fromDate, &toDateParsed, productType, currency, status,
		organisation, email, intAccountID, evaluateMembership, orderByPaymentDate)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": orders, "success": true})
}
