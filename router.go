package main

import (
	"github.com/gin-gonic/gin"
)

func initRouter() *gin.Engine {
	if Conf["MODE"] == "PROD" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	orders := r.Group("/orders")
	{
		orders.GET("/", listOrders)
		orders.POST("/new", createOrder)
		orders.POST("/newandpay", createOrderAndPay)
	}

	return r
}
