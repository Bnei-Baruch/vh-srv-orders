package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func handleSpecialHardDeleteByEmail(ctx *gin.Context) {
	email := ctx.Param("email")

	err, rowsAffected := hardDeleteSpecialByEmail(ctx, email)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true, "data": rowsAffected})
}
