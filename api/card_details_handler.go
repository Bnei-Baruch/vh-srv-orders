package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleCardDetailGetByID(c *gin.Context) {
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

	cardDetail, err := o.repo.GetCardDetailById(c, intID)

	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Card detail not found"})
			return
		}
		fmt.Println("Error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": cardDetail, "success": true})
		return
	}
}

func (o *OrdersAPI) handleCardDetailSoftDeleteByID(c *gin.Context) {
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

	err = o.repo.SoftDeleteCardDetailById(c, intID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handleCardDetailCreate(c *gin.Context) {
	var req repo.CardDetails
	errRequest := c.BindJSON(&req)

	if errRequest != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	cardDetailId, err := o.repo.CreateCardDetailsAndGetId(c, req)

	if err != nil {
		if errors.Is(err, fmt.Errorf("invalid body")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": cardDetailId, "success": true})
}

func (o *OrdersAPI) handleCardDetailUpdateByID(c *gin.Context) {
	var req repo.CardDetails
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

	err = o.repo.PatchCardDetailsById(c, req, intID)

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

func (o *OrdersAPI) handleCardDetailsFetchAll(c *gin.Context) {
	skip := c.Query("skip")
	limit := c.Query("limit")

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

	orders, err := o.repo.GetAllCardDetails(c, intSkip, intLimit)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": orders, "success": true})
}
