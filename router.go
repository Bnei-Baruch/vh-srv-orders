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
		orders.POST("/new", handleOrdersCreate)
		orders.POST("/update", handleUpdateOrders)
		orders.POST("/paid", handleOrdersPaid)
		orders.POST("/newandpay", handleCreateOrderAndPay)
		orders.POST("/renew", handleOrdersRenew)
		orders.GET("/count/:filter", handleOrdersCount)
		orders.POST("/flag", handleOrdersFlag)
	}

	payments := r.Group("/payments")
	{
		payments.GET("/all/:email", handlePaymentFetchByEmail)
		payments.POST("/", handleCreatePayment)
		payments.GET("/payment/:paramx", handlePaymentFetchViaParamX)
		payments.POST("/update", handleUpdatePayment)
		payments.GET("/activities", handleGetActivities)
	}

	baseV2Path := r.Group("/v2")

	participant := baseV2Path.Group("/payment")
	{
		participant.PATCH("/", handlePaymentUpdate)
	}

	r.GET("/status/:email", Status)

	return r
}
