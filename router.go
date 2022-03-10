package main

import (
	"fmt"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/location"
	"github.com/gin-gonic/gin"
)

func initRouter() *gin.Engine {
	if Conf["MODE"] == "PROD" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(location.Default())

	if os.Getenv("CORSISON") == "YES" {
		fmt.Println("CORS ACTIVE")
		r.Use(cors.Default())
	}

	orders := r.Group("/orders")
	{
		orders.POST("/new", handleOrdersCreate)            // Tested
		orders.POST("/update", handleUpdateOrders)         // Tested
		orders.POST("/paid", handleOrdersPaid)             // Tested
		orders.POST("/newandpay", handleCreateOrderAndPay) // Tested
		orders.POST("/renew", handleOrdersRenew)           // Tested
		orders.GET("/count/:filter", handleOrdersCount)    // Tested
		orders.POST("/flag", handleOrdersFlag)             // Tested
	}

	payments := r.Group("/payments")
	{
		payments.POST("/", handleCreatePayment)       // Tested
		payments.POST("/update", handleUpdatePayment) // Tested
	}

	r.GET("/status/:email", Status) // Tested

	return r
}
