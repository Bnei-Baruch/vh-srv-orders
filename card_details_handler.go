package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

func handleCardDetailGetByID(ctx *gin.Context) {
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

	cardDetail, err := getCardDetailById(ctx, intID)

	if err != nil {
		if err == pgx.ErrNoRows {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "Card detail not found"})
			return
		}
		fmt.Println("Error:", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": cardDetail, "success": true})
		return
	}
}

func handleCardDetailSoftDeleteByID(ctx *gin.Context) {
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

	err = softDeleteCardDetailById(ctx, intID)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func handleCardDetailCreateByID(ctx *gin.Context) {
	var req CardDetails
	errRequest := ctx.BindJSON(&req)

	if errRequest != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	accountId, err := createCardDetailsAndGetId(ctx, req)

	if err != nil {
		if errors.Is(err, fmt.Errorf("invalid body")) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": accountId, "success": true})
}

func handleCardDetailUpdateByID(ctx *gin.Context) {
	var req CardDetails
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

	err = patchCardDetailsById(ctx, req, intID)

	if err != nil {
		if errors.Is(err, fmt.Errorf("invalid body")) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
}

func handleCardDetailsFetchAll(ctx *gin.Context) {
	skip := ctx.Query("skip")
	limit := ctx.Query("limit")

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

	orders, err := GetAllCardDetails(ctx, intSkip, intLimit)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": orders, "success": true})
}
