package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleV2OrderCreate(c *gin.Context) {
	var req repo.Order
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AccountID.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing AccountID"})
		return
	}
	userIdString := strconv.Itoa(req.AccountID.Int)
	if !o.isUserOrHasAnyRole(c, userIdString, common.RoleRoot, common.RoleAdmin) {
		return
	}

	orderID, err := o.repo.CreateV2Order(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, common.ErrInvalidValues) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.CreateV2Order: %w", err))
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": orderID, "success": true})
}

func (o *OrdersAPI) handleOrderDeleteByID(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = o.repo.SoftDeleteOrderByID(c.Request.Context(), id)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.SoftDeleteOrderByID: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleOrderUpdateByID(c *gin.Context) {

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	if !isAuthUser {
		return
	}

	var req repo.Order
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !isAdmin {
		account, err := o.repo.GetAccountForOrderID(c, uint(id))
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetAccountForOrderID: %w", err))
			return
		}
		if account.UserKey.String != keycloakId {
			c.Status(http.StatusForbidden)
			return
		}
	}

	err = o.repo.PatchOrderByID(c.Request.Context(), req, id)
	if err != nil {
		if errors.Is(err, common.ErrInvalidValues) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.PatchOrderByID: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
}

func (o *OrdersAPI) handleOrderGetByID(c *gin.Context) {

	isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	if !isAuthUser {
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}
	if !isAdmin {
		account, err := o.repo.GetAccountForOrderID(c, uint(id))
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetAccountForOrderID: %w", err))
			return
		}
		if account.UserKey.String != keycloakId {
			c.Status(http.StatusForbidden)
			return
		}
	}

	order, err := o.repo.GetOrderByID(c.Request.Context(), uint(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetOrderByID: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": order, "success": true})
}

func (o *OrdersAPI) handleOrdersRenew(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	type req struct {
		User string `json:"user"`
		Key  string `json:"key"`
	}

	var body req
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if body.User == "admin" && (body.Key == "t" || body.Key == "e") {
		count, err := o.repo.ChargeOrdersToRenew(c.Request.Context(), body.Key)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.ChargeOrdersToRenew: %w", err))
			return
		}

		c.JSON(http.StatusOK, gin.H{"count": count})
	} else {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "You are not allowed here"})
	}
}

func (o *OrdersAPI) handleOrdersUpdateToken(c *gin.Context) {
	var (
		req repo.RequestUpdateToken
		err error
	)
	if err = c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.ParamX != fmt.Sprintf("new_token_%s", strconv.Itoa(req.OrderId)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wrong ParamX"})
		return
	}

	account, err := o.repo.GetAccountForOrderID(c.Request.Context(), uint(req.OrderId))
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAccountForOrderID: %w", err))
		return
	}

	if !o.isUserOrHasAnyRole(c, strconv.Itoa(account.ID), common.RoleRoot, common.RoleAdmin) {
		return
	}

	if err = o.repo.UpdateOrdersToken(c.Request.Context(), req); err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.UpdateOrdersToken: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
}

func (o *OrdersAPI) handleOrderFetch(c *gin.Context) {
	isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	if !isAuthUser {
		return
	}

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

	intSkip, err := strconv.Atoi(skip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

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
	if isAdmin {
		keycloakId = "" //We should send keycloakId only is user is not admin
	}

	orders, err := o.repo.GetAllOrders(c.Request.Context(), intSkip, intLimit, fromDate, &toDateParsed, productType,
		currency, status, organisation, email, intAccountID, keycloakId, evaluateMembership, orderByPaymentDate)

	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAllOrders: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": orders, "success": true})
}

func (o *OrdersAPI) handleCreateOffline(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	var req repo.OfflinePaymentRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error(), "success": false})
		return
	}
	if !req.KeycloakID.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing KeycloakID.", "success": false})
		return
	}
	if !req.Currency.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing Currency.", "success": false})
		return
	}
	if !req.PaymentMethod.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing PaymentMethod.", "success": false})
		return
	}
	if !req.PaymentDate.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing PaymentDate.", "success": false})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount must be postive and not zero.", "success": false})
		return
	}
	if req.Quantity < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The quantity must be a minimum of one.", "success": false})
		return
	}

	accountID, err := o.repo.GetOrCreateAccount(c, req.KeycloakID.String)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The given KeycloakID is not found.", "success": false})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetAccountIDByKeycloakID: %w", err))
		}
		return
	}

	order := repo.Order{
		Type:          null.StringFrom(common.OrderTypeRegular),
		ProductType:   null.StringFrom(common.ProductTypeGlobalMembership),
		AccountID:     null.IntFrom(accountID),
		Amount:        null.Float64From(req.Amount),
		Currency:      req.Currency,
		SKU:           null.StringFrom(common.ProductSKU40037),
		Status:        null.StringFrom(common.OrderStatusPaid),
		OrderLanguage: req.Language,
		PaymentDate:   req.PaymentDate,
		Quantity:      null.IntFrom(req.Quantity),
		Note:          req.Note,
	}
	orderId, err := o.repo.CreateV2Order(c.Request.Context(), order)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CreateV2Order: %w", err))
		return
	}

	payment := repo.RequestOrder{
		Amount:               null.Float64From(req.Amount),
		Currency:             req.Currency,
		PaymentType:          null.StringFrom(common.PaymentTypeOffline),
		PaymentStatus:        null.StringFrom(common.PaymentStatusSuccess),
		PaymentMethod:        req.PaymentMethod,
		OfflinePaymentStatus: null.StringFrom(common.PaymentStatusSuccess),
	}

	_, err = o.repo.CreatePayment(c.Request.Context(), payment, orderId)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CreatePayment: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Created!", "success": true})
}
