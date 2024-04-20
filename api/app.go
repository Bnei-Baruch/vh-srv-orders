package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
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
	a.initSentry()
	a.initEventEmitter()
	a.initDB()
	a.ordersAPI = NewOrdersAPI(a.repo)
	a.initGinEngine()
}

func (a *App) initEventEmitter() {
	if common.Config.NatsUrl != "" {
		slog.Info("initializing events emitter")
		var err error
		a.eventEmitter, err = events.CreateEmitter()
		if err != nil {
			utils.LogFatal("events.CreateEmitter", slog.Any("err", err))
		}
	}
}

func (a *App) initDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	a.repo, err = repo.NewOrdersDB(ctx, a.eventEmitter)
	if err != nil {
		utils.LogFatal("connect to db", slog.Any("err", err))
	}

	err = repo.SyncDBStructInsertionAndMigrations()
	if err != nil {
		utils.LogFatal("db migrations", slog.Any("err", err))
	}

	slog.Info("db connected and migrated")
}

func (a *App) initSentry() {
	err := sentry.Init(sentry.ClientOptions{
		Release:          common.GitSHA,
		Environment:      common.Config.Env,
		AttachStacktrace: true,
	})
	if err != nil {
		utils.LogFatal("sentry.Init", slog.Any("err", err))
	}
}

func (a *App) initGinEngine() {
	gin.SetMode(common.Config.Mode)
	a.gEngine = gin.New()

	// middleware
	a.gEngine.Use(
		middleware.Logging(),
		middleware.Recovery(),
		sentrygin.New(sentrygin.Options{Repanic: true}),
		middleware.Sentry(),
		middleware.EventsBuilder(),
	)
	if gin.IsDebugging() {
		a.gEngine.Use(cors.Default())
	}

	// routes
	orders := a.gEngine.Group("/orders")
	{
		orders.POST("/paid", a.ordersAPI.handleTransactionPaid)     // vh-payments (deprecated in favor of PATCH /v2/transaction)
		orders.POST("/renew", a.ordersAPI.handleOrdersRenew)        // charge (python)
		orders.GET("/count/:filter", a.ordersAPI.handleOrdersCount) // charge (python)
		orders.POST("/flag", a.ordersAPI.handleOrdersFlag)          // charge (python)
	}

	payments := a.gEngine.Group("/payments")
	{
		payments.GET("/all/:email", a.ordersAPI.handlePaymentFetchByEmail)
		payments.GET("/payment/:paramx", a.ordersAPI.handlePaymentFetchViaParamX)
		payments.GET("/activities", a.ordersAPI.handleGetActivities)
	}

	baseV2Path := a.gEngine.Group("/v2")

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

	a.gEngine.GET("/status/:email", a.ordersAPI.status)
}

func (a *App) Run() {
	if err := a.gEngine.Run(":" + common.Config.Port); err != nil {
		utils.LogFatal("gin.Run", slog.Any("err", err))
	}
}

func (a *App) Shutdown() {
	a.repo.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	a.eventEmitter.Close(ctx)
	sentry.Flush(2 * time.Second)
}
