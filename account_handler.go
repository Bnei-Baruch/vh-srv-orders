package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	uuid "github.com/satori/go.uuid"
)

func handleGetAccount(ctx *gin.Context) {
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

	account, err := getAccount(ctx, intID, "")

	if err != nil {
		if err == pgx.ErrNoRows {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
			return
		}
		fmt.Println("Error:", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": account, "success": true})
		return
	}
}

func handleCreateAccount(ctx *gin.Context) {
	var req Account
	errRequest := ctx.BindJSON(&req)

	if errRequest != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"Error": errRequest.Error()})
		return
	}

	accountId, err := createAccount(ctx, req)

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

func handlePatchAccount(ctx *gin.Context) {
	var req Account
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

	err = patchAccount(ctx, req, intID)

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

func handleDeleteAccount(ctx *gin.Context) {
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

	err = softDeleteAccount(ctx, intID)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func handleHardDeleteAccount(ctx *gin.Context) {
	id := ctx.Param("id")

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
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER or UUID", "success": false})
			return
		}
	}

	err = hardDeleteAllUserDataByAccountID(ctx, intID, id)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}
