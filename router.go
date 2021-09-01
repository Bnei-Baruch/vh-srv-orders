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

	orders := r.Group("/orders")
	{
		orders.GET("/", handleOrdersList)
		orders.POST("/new", handleOrdersCreate)
		orders.POST("/update", handleUpdateOrders)
		orders.POST("/paid", handleOrdersPaid)
		orders.POST("/newandpay", handleCreateOrderAndPay)
		orders.POST("/renew", handleOrdersRenew)
		orders.POST("/renewbyid/:id", handleOrdersRenewByID)
		orders.GET("/count/:filter", handleOrdersCount)
		orders.GET("/count/:filter/:month", handleOrdersCountByMonth)
		orders.GET("/count/:filter/:month/:currency", handleOrdersCountByMonthAndCurrency)
		orders.GET("/update/:id/:status", handleOrdersUpdateStatus)
		orders.POST("/note/:id/:note", handleOrdersAnnotate)
		orders.POST("/flag", handleOrdersFlag)
		orders.POST("/clean/:month", handleOrdersClean)
		orders.GET("/test", handleOrdersTest)
	}

	vh := r.Group("/vh")
	{
		vh.GET("/ispaid/:id", handleVHisPaid)
	}

	payments := r.Group("/payments")
	{
		payments.POST("/", handleCreatePayment)
		payments.POST("/update", handleUpdatePayment)
		payments.GET("/count/:filter/:month", handlePaymentsCountByMonth)
		payments.GET("/count/:filter/:month/:currency", handlePaymentsCountByMonthAndCurrency)
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
		accounts.GET("/test", handleAccountsTest)
	}

	admin := r.Group("/admin")
	{
		admin.GET("/subscriptions", handleAdminSubscriptions)
		admin.POST("/stats", handleAdminStats)
		admin.GET("/subscriptions/:id", handleAdminSubscriptionByID)
		admin.GET("/payments", handleAdmin)
		admin.GET("/payments/:id", handleAdmin)
		admin.GET("/accounts", handleAdmin)
		admin.GET("/accounts/:id", handleAdmin)
		admin.GET("/reports/:id", handleAdmin)
	}

	r.GET("/status/:email", Status)

	return r
}
