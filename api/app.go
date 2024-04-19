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

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type App struct {
	repo         repo.OrdersRepository
	eventEmitter events.EventEmitter
	ordersAPI    *OrdersAPI
	gEngine      *gin.Engine
}

func NewApp() *App {
	return new(App)
}

func (a *App) Initialize() {
	var err error

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		log.Fatalf("Error creating event emitter: %v\n", err)
	}

	a.repo, err = repo.NewOrdersDB(ctx, a.eventEmitter)
	if err != nil {
		log.Fatalf("Error connecting to orders db: %s \n***\n %s \n ***", err, repo.GetDBURL())
	}
	fmt.Println("Connected to orders db")

	err = repo.SyncDBStructInsertionAndMigrations()
	if err != nil {
		log.Fatalf("Error migrating orders db: %s \n***\n %s \n ***", err, repo.GetDBURL())
	}
	fmt.Println("Migrated orders db")

	a.ordersAPI = NewOrdersAPI(a.repo)
	a.initGinEngine()
}

func (a *App) initGinEngine() {
	if common.Config.Mode == "PROD" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(location.Default())
	r.Use(middleware.EventsBuilder())

	if os.Getenv("CORSISON") == "YES" {
		fmt.Println("CORS ACTIVE")
		r.Use(cors.Default())
	}

	// routes
	orders := r.Group("/orders")
	{
		orders.POST("/paid", a.ordersAPI.handleTransactionPaid)     // vh-payments (deprecated in favor of PATCH /v2/transaction)
		orders.POST("/renew", a.ordersAPI.handleOrdersRenew)        // charge (python)
		orders.GET("/count/:filter", a.ordersAPI.handleOrdersCount) // charge (python)
		orders.POST("/flag", a.ordersAPI.handleOrdersFlag)          // charge (python)
	}

	payments := r.Group("/payments")
	{
		payments.GET("/all/:email", a.ordersAPI.handlePaymentFetchByEmail)
		payments.GET("/payment/:paramx", a.ordersAPI.handlePaymentFetchViaParamX)
		payments.GET("/activities", a.ordersAPI.handleGetActivities)
	}

	baseV2Path := r.Group("/v2")

	account := baseV2Path.Group("/account")
	{
		account.POST("/", a.ordersAPI.handleCreateAccount)
		account.GET("/:id", a.ordersAPI.handleGetAccount)
		account.PATCH("/:id", a.ordersAPI.handlePatchAccount)
		account.DELETE("/:id", a.ordersAPI.handleDeleteAccount)
		account.DELETE("/:id/hard", a.ordersAPI.handleHardDeleteAccount)
	}
	baseV2Path.GET("/accounts", a.ordersAPI.handleFetchAccounts)

	order := baseV2Path.Group("/order")
	{
		order.GET("/:id", a.ordersAPI.handleOrderGetByID)
		order.DELETE("/:id", a.ordersAPI.handleOrderDeleteByID)
		order.POST("/", a.ordersAPI.handleV2OrderCreate)
		order.POST("/offline", a.ordersAPI.handleCreateOffline)
		order.PATCH("/:id", a.ordersAPI.handleOrderUpdateByID)
	}
	baseV2Path.GET("/orders", a.ordersAPI.handleOrderFetch)

	payment := baseV2Path.Group("/payment")
	{
		payment.PATCH("/", a.ordersAPI.handlePaymentUpdate)
		payment.DELETE("/:id", a.ordersAPI.handlePaymentDelete)
		payment.GET("/:id", a.ordersAPI.handlePaymentFetchByID)
	}
	baseV2Path.GET("/payments", a.ordersAPI.handlePaymentFetch)

	transaction := baseV2Path.Group("/transaction")
	{
		transaction.GET("/:id", a.ordersAPI.handleTransactionGetByID)
		transaction.PATCH("/", a.ordersAPI.handleTransactionPaid)
		transaction.POST("/", a.ordersAPI.handleTransactionOrderAndPay)
	}

	userCardDetails := baseV2Path.Group("/card_detail")
	{
		userCardDetails.GET("/:id", a.ordersAPI.handleCardDetailGetByID)
		userCardDetails.DELETE("/:id", a.ordersAPI.handleCardDetailSoftDeleteByID)
		userCardDetails.PATCH("/:id", a.ordersAPI.handleCardDetailUpdateByID)
		userCardDetails.POST("/", a.ordersAPI.handleCardDetailCreate)
	}
	baseV2Path.GET("/card_details", a.ordersAPI.handleCardDetailsFetchAll)

	special := baseV2Path.Group("/special")
	{
		special.DELETE("/:email", a.ordersAPI.handleSpecialHardDeleteByEmail)
		special.GET("/:email", a.ordersAPI.handleSpecialGetByEmail)
	}

	operation := baseV2Path.Group("/operation")
	{
		operation.POST("/", a.ordersAPI.handleOperationCreate)
		operation.POST("/revert", a.ordersAPI.handleOperationRevert)
	}

	r.GET("/status/:email", a.ordersAPI.status)

	a.gEngine = r
}

func (a *App) Run() {
	if err := a.gEngine.Run(common.Config.Port); err != nil {
		log.Fatalf("server stopped: %s", err)
	}
}

func (a *App) Shutdown() {
	a.repo.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	a.eventEmitter.Close(ctx)
}
