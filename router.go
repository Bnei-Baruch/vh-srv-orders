package main

import (
	"github.com/gin-contrib/location"
	"github.com/gin-gonic/gin"
)

func initRouter() *gin.Engine {
	if Conf["MODE"] == "PROD" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(location.Default())
	r.Use(CORSMiddleware())
	orders := r.Group("/orders")
	{
		orders.GET("/", listOrders)
		orders.POST("/new", handleCreateOrder)
		orders.POST("/paid", handlePaid)
		orders.POST("/newandpay", handleCreateOrderAndPay)
		orders.POST("/renew/:month", handleRenew)
		orders.GET("/count/:filter", handleCount)
		orders.GET("/count/:filter/:month", handleCountByMonth)
		orders.GET("/count/:filter/:month/:currency", handleCountByMonthAndCurrency)
		orders.GET("/update/:id/:status", handleUpdateOrderStatus)
		orders.POST("/note/:id/:note", handleAnnotate)
		orders.POST("/flag/:flag", handleFlag)
	}

	payments := r.Group("/payments")
	{
		payments.GET("/count/:filter/:month", handlePaymentCountByMonth)
		payments.GET("/count/:filter/:month/:currency", handlePaymentCountByMonthAndCurrency)
	}

	test := r.Group("/test")
	{
		test.GET("/", handleTest)
	}

	pelecard := r.Group("/pelecard")
	{
		pelecard.GET("/:status", handlePelecardStatus)
	}

	accounts := r.Group("/accounts")
	{
		accounts.GET("/list", listAll)
		accounts.GET("/ping", pingAccounts)
		accounts.POST("/ping", echoAccounts)
		accounts.POST("/new", new)
		accounts.POST("/update/:id", update)
		accounts.GET("/findByEmail/:email", findByEmail)
		accounts.GET("/find/:id", find)
		accounts.GET("/count", handleCountAccounts)
		accounts.POST("/delete/:id", delete)
	}

	return r
}

// CORSMiddleware ...
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, UPDATE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
		} else {
			c.Next()
		}
	}
}
