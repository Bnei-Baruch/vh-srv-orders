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
		orders.GET("/", handleOrdersList) // rm
		orders.POST("/new", handleOrdersCreate)
		orders.POST("/update", handleUpdateOrders)
		orders.POST("/paid", handleOrdersPaid)
		orders.POST("/newandpay", handleCreateOrderAndPay)
		orders.POST("/renew", handleOrdersRenew)
		orders.POST("/renewbyid/:id", handleOrdersRenewByID) //rm
		orders.GET("/count/:filter", handleOrdersCount)
		orders.GET("/count/:filter/:month", handleOrdersCountByMonth)                      // rm
		orders.GET("/count/:filter/:month/:currency", handleOrdersCountByMonthAndCurrency) // rm
		orders.GET("/update/:id/:status", handleOrdersUpdateStatus)                        // rm
		orders.POST("/note/:id/:note", handleOrdersAnnotate)                               // rm
		orders.POST("/flag", handleOrdersFlag)
		orders.POST("/clean/:month", handleOrdersClean) // rm
		orders.GET("/test", handleOrdersTest)           // rm
	}

	vh := r.Group("/vh") // rm
	{
		vh.GET("/ispaid/:id", handleVHisPaid)
	}

	payments := r.Group("/payments")
	{
		payments.POST("/", handleCreatePayment)
		payments.POST("/update", handleUpdatePayment)
		payments.GET("/count/:filter/:month", handlePaymentsCountByMonth)                      // rm
		payments.GET("/count/:filter/:month/:currency", handlePaymentsCountByMonthAndCurrency) //rm
	}

	accounts := r.Group("/accounts")
	{
		accounts.GET("/list", listAll)                   // rm
		accounts.GET("/ping", pingAccounts)              // rm
		accounts.POST("/ping", echoAccounts)             // rm
		accounts.POST("/new", new)                       // rm
		accounts.POST("/update/:id", update)             // rm
		accounts.GET("/findByEmail/:email", findByEmail) // rm
		accounts.GET("/find/:id", find)                  // rm
		accounts.GET("/count", handleCountAccounts)      // rm
		accounts.POST("/delete/:id", delete)             // rm
		accounts.GET("/test", handleAccountsTest)        // rm
	}

	admin := r.Group("/admin") // rm
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
