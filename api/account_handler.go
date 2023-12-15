package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	uuid "github.com/satori/go.uuid"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleGetAccount(c *gin.Context) {
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

	account, err := o.repo.GetAccount(c.Request.Context(), intID, "")

	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
			return
		}
		fmt.Println("Error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": account, "success": true})
		return
	}
}

func (o *OrdersAPI) handleCreateAccount(c *gin.Context) {
	var req repo.Account
	errRequest := c.BindJSON(&req)
	if errRequest != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	accountId, err := o.repo.CreateAccount(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, common.ErrInvalidBody) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": accountId, "success": true})
}

func (o *OrdersAPI) handleFetchAccounts(c *gin.Context) {
	skip := c.Query("skip")
	limit := c.Query("limit")
	email := c.Query("email")

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

	accounts, err := o.repo.GetAllAccounts(c.Request.Context(), intSkip, intLimit, email)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": accounts, "success": true})
}

func (o *OrdersAPI) handlePatchAccount(c *gin.Context) {
	var req repo.Account
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

	err = o.repo.PatchAccount(c.Request.Context(), req, intID)

	if err != nil {
		if errors.Is(err, fmt.Errorf("invalid body")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
}

func (o *OrdersAPI) handleDeleteAccount(c *gin.Context) {
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

	err = o.repo.SoftDeleteAccount(c.Request.Context(), intID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleHardDeleteAccount(c *gin.Context) {
	id := c.Param("id")

	// check if id is string or integer
	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		// if id is string, then check if it is uuid
		_, err = uuid.FromString(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER or UUID", "success": false})
			return
		}
	}

	err = o.repo.HardDeleteAllUserDataByAccountID(c.Request.Context(), intID, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}
