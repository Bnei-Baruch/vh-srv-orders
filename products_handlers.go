package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func handleProductbyID(c *gin.Context) {
	paramProductID := string(c.Params.ByName("id"))
	productID, _ := strconv.ParseInt(paramProductID, 10, 64)
	//productData := getProductByID(productID)
	productData := initProductConvention()
	c.JSON(http.StatusOK, gin.H{"productID": productID, "data": productData})
}
