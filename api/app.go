package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/location"
	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type App struct {
	OrdersAPI *OrdersAPI
	DB        repo.OrdersRepository
	gEngine   *gin.Engine
}

func NewApp() *App {
	return new(App)
}

func (a *App) Initialize() {
	var err error

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a.DB, err = repo.NewOrdersDB(ctx)
	if err != nil {
		log.Fatalf("Error connecting to orders db: %s \n***\n %s \n ***", err, repo.GetDBURL())
	}
	fmt.Println("Connected to orders db")

	err = repo.SyncDBStructInsertionAndMigrations()
	if err != nil {
		log.Fatalf("Error migrating orders db: %s \n***\n %s \n ***", err, repo.GetDBURL())
	}
	fmt.Println("Migrated orders db")

	a.OrdersAPI = NewOrdersAPI(a.DB)
	a.initGinEngine()
}

func (a *App) initGinEngine() {
	if common.Config.Mode == "PROD" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(location.Default())

	if os.Getenv("CORSISON") == "YES" {
		fmt.Println("CORS ACTIVE")
		r.Use(cors.Default())
	}

	// routes
	orders := r.Group("/orders")
	{
		orders.POST("/new", a.OrdersAPI.handleOrdersCreate)
		orders.POST("/update", a.OrdersAPI.handleUpdateOrders)
		orders.POST("/paid", a.OrdersAPI.handleOrdersPaid)
		orders.POST("/newandpay", a.OrdersAPI.handleCreateOrderAndPay)
		orders.POST("/renew", a.OrdersAPI.handleOrdersRenew)
		orders.GET("/count/:filter", a.OrdersAPI.handleOrdersCount)
		orders.POST("/flag", a.OrdersAPI.handleOrdersFlag)
	}

	payments := r.Group("/payments")
	{
		payments.GET("/all/:email", a.OrdersAPI.handlePaymentFetchByEmail)
		//payments.POST("/", a.OrdersAPI.handleCreatePayment)
		payments.GET("/payment/:paramx", a.OrdersAPI.handlePaymentFetchViaParamX)
		//payments.POST("/update", a.OrdersAPI.handleUpdatePayment)
		payments.GET("/activities", a.OrdersAPI.handleGetActivities)
	}

	baseV2Path := r.Group("/v2")

	payment := baseV2Path.Group("/payment")
	{
		// payments.POST("/", handleCreatePayment) // uncomment when endpoint defined about /payments [POST] is no longer used
		payment.PATCH("/", a.OrdersAPI.handlePaymentUpdate)
		payment.DELETE("/:id", a.OrdersAPI.handlePaymentDelete)
		payment.GET("/:id", a.OrdersAPI.handlePaymentFetchByID)
	}
	baseV2Path.GET("/payments", a.OrdersAPI.handlePaymentFetch)

	account := baseV2Path.Group("/account")
	{
		account.POST("/", a.OrdersAPI.handleCreateAccount)
		account.GET("/:id", a.OrdersAPI.handleGetAccount)
		account.PATCH("/:id", a.OrdersAPI.handlePatchAccount)
		account.DELETE("/:id", a.OrdersAPI.handleDeleteAccount)
		account.DELETE("/:id/hard", a.OrdersAPI.handleHardDeleteAccount)
	}
	baseV2Path.GET("/accounts", a.OrdersAPI.handleFetchAccounts)

	order := baseV2Path.Group("/order")
	{
		order.GET("/:id", a.OrdersAPI.handleOrderGetByID)
		order.DELETE("/:id", a.OrdersAPI.handleOrderDeleteByID)
		order.POST("/", a.OrdersAPI.handleV2OrderCreate)
		order.PATCH("/:id", a.OrdersAPI.handleOrderUpdateByID)
	}
	baseV2Path.GET("/orders", a.OrdersAPI.handleOrderFetch)

	userCardDetails := baseV2Path.Group("/card_detail")
	{
		userCardDetails.GET("/:id", a.OrdersAPI.handleCardDetailGetByID)
		userCardDetails.DELETE("/:id", a.OrdersAPI.handleCardDetailSoftDeleteByID)
		userCardDetails.PATCH("/:id", a.OrdersAPI.handleCardDetailUpdateByID)
		userCardDetails.POST("/", a.OrdersAPI.handleCardDetailCreate)
	}
	baseV2Path.GET("/card_details", a.OrdersAPI.handleCardDetailsFetchAll)

	transaction := baseV2Path.Group("/transaction")
	{
		transaction.GET("/:id", a.OrdersAPI.handleTransactionGetByID)
		transaction.PATCH("/", a.OrdersAPI.handleTransactionPaid)
		transaction.POST("/", a.OrdersAPI.handleTransactionOrderAndPay)
	}

	special := baseV2Path.Group("/special")
	{
		special.DELETE("/:email", a.OrdersAPI.handleSpecialHardDeleteByEmail)
		special.GET("/:email", a.OrdersAPI.handleSpecialGetByEmail)
	}

	operation := baseV2Path.Group("/operation")
	{
		operation.POST("/", a.OrdersAPI.handleOperationCreate)
		operation.POST("/revert", a.OrdersAPI.handleOperationRevert)
	}

	r.GET("/status/:email", a.OrdersAPI.status)

	a.gEngine = r
}

func (a *App) Run() {
	if err := a.gEngine.Run(common.Config.Port); err != nil {
		log.Fatalf("server stopped: %s", err)
	}
}

func (a *App) Shutdown() {
	a.DB.Close()
}
