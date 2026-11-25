package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hellofresh/health-go/v5"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	healthnats "github.com/hellofresh/health-go/v5/checks/nats"
	healthpgx "github.com/hellofresh/health-go/v5/checks/pgx5"

	"github.com/Bnei-Baruch/vh-srv-orders/api/middleware"
	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/domain"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/profiles"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

type App struct {
	// Concrete, with the same promoted-pgx caveat as OrdersAPI.repo. Close() in
	// Shutdown is the one pool method this type should call.
	repo *repo.OrdersDB
	// repoClose is what Shutdown calls to close the repo: set with repo by
	// initDB, and replaced in tests by a Close that blocks or counts.
	repoClose           func()
	eventEmitter        events.EventEmitter
	eventListener       *profiles.EventListener
	domainEventsHandler *domain.EventsHandler
	ordersAPI           *OrdersAPI
	couponAPI           *CouponAPI
	gEngine             *gin.Engine

	shutdownOnce sync.Once

	// stop undoes the signal registration, so a fatal can hand signals back to
	// their default disposition before it drains. Set by Initialize; nil in
	// tests that build an App directly, which fatal tolerates.
	stop func()
}

func NewApp() *App {
	return new(App)
}

// Initialize builds the app, stopping early if the process is already being
// asked to shut down.
func (a *App) Initialize(ctx context.Context, stop func()) error {
	a.stop = stop

	steps := []struct {
		name string
		run  func()
	}{
		{"sentry", a.initSentry},
		{"events emitter", a.initEventEmitter},
		{"database", func() { a.initDB(ctx) }},
		{"events listener", a.initEventListener},
		{"gin engine", func() { a.initGinEngine(ctx) }},
		{"health", a.initHealth},
	}

	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("aborted before %s: %w", step.name, err)
		}
		step.run()
	}

	return nil
}

// initEventEmitter always builds one: with no NATS_URL (the documented default)
// the field used to stay a nil interface, and the first Emit or Shutdown panicked.
func (a *App) initEventEmitter() {
	slog.Info("initializing events emitter")

	var err error
	a.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		a.fatal("events.CreateEmitter", slog.Any("err", err))
	}
}

// initDB takes the startup context so a signal reaches the connect, rather than
// only the gap before it.
var dbConnectTimeout = 30 * time.Second

func (a *App) initDB(ctx context.Context) {
	connectCtx, cancel := context.WithTimeout(ctx, dbConnectTimeout)
	defer cancel()

	// Through a local: NewOrdersDB returns a concrete *repo.OrdersDB, and on
	// failure that nil pointer boxes into a non-nil interface — Shutdown's guard
	// would pass and its Close would panic on the nil receiver.
	ordersDB, err := repo.NewOrdersDB(connectCtx, a.eventEmitter)
	if err != nil {
		// The parent, not connectCtx: a connect that times out is a failure,
		// not a shutdown, and must not leave the server running with no repo.
		if ctx.Err() != nil {
			slog.Info("db connect abandoned, shutting down", slog.Any("err", err))
			return
		}
		a.fatal("connect to db", slog.Any("err", err))
	}
	a.repo = ordersDB

	err = repo.SyncDBStructInsertionAndMigrations()
	if err != nil {
		a.fatal("db migrations", slog.Any("err", err))
	}

	slog.Info("db connected and migrated")
}

func (a *App) initEventListener() {
	if common.Config.NatsUrl != "" {
		slog.Info("initializing events listener")

		var err error
		a.eventListener, err = profiles.NewEventListener()
		if err != nil {
			a.fatal("profiles.NewEventListener", slog.Any("err", err))
		}

		a.domainEventsHandler = domain.NewEventsHandler(a.repo)
		a.eventListener.RegisterHandler(a.domainEventsHandler.HandleProfilesEvent)

		if err = a.eventListener.Run(); err != nil {
			a.fatal("eventListener.Run", slog.Any("err", err))
		}
	}
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

func (a *App) initGinEngine(ctx context.Context) {
	gin.SetMode(common.Config.Mode)
	a.gEngine = gin.New()
	issuerUrl := fmt.Sprintf("%s/auth/realms/%s", common.Config.KeycloakServerUrl, common.Config.KeycloakRealm)
	tokenVerifier, err := middleware.NewFailoverOIDCTokenVerifier(ctx, issuerUrl)
	if err != nil {
		// Same as initDB: a signal during the fetch is a shutdown, not a
		// misconfiguration, and serverFn already reports it as one.
		if ctx.Err() != nil {
			slog.Info("jwks fetch abandoned, shutting down", slog.Any("err", err))
			return
		}
		a.fatal("middleware.NewFailoverOIDCTokenVerifier", slog.Any("err", err))
	}

	// middleware
	a.gEngine.Use(
		middleware.Logging(),
		middleware.Recovery(),
		sentrygin.New(sentrygin.Options{Repanic: true}),
		middleware.Sentry(),
	)
	if gin.IsDebugging() {
		a.gEngine.Use(cors.New(cors.Config{
			AllowAllOrigins:  true,
			AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
			ExposeHeaders:    []string{"Content-Length"},
			AllowCredentials: true,
			MaxAge:           12 * time.Hour,
		}))
	}
	a.gEngine.Use(
		middleware.EventsBuilder(),
		middleware.TokenSource(),
	)

	a.initRoutes(middleware.Authentication(tokenVerifier))
}

// initRoutes registers the public routes, then auth, then everything else. A
// gin middleware added with Use applies only to routes registered after it, so
// the order here is what keeps every other route behind auth.
func (a *App) initRoutes(auth ...gin.HandlerFunc) {
	// Constructed here so owning a group and registering its routes are one
	// step. A route registered on a nil group compiles and starts fine, then
	// panics on the first request — this branch carried that bug while only
	// Initialize remembered and NewTestApp did not, fixed in 9cd6a5f. It never
	// left the branch.
	a.ordersAPI = NewOrdersAPI(a.repo)
	a.couponAPI = NewCouponAPI(a.repo)

	// Public: called server to server by external_payments, which has no
	// Keycloak identity here. Trusts nothing in the body; see the handler.
	a.gEngine.POST("/checkout/notify", a.ordersAPI.handleCheckoutNotify)

	a.gEngine.Use(auth...)

	// routes
	orders := a.gEngine.Group("/orders")
	{
		orders.POST("/paid", a.ordersAPI.handleTransactionPaid)     // vh-payments (deprecated in favor of PATCH /v2/transaction)
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
		account.GET("/email/:email", a.ordersAPI.handleGetAccountByEmail)
		account.PATCH("/:id", a.ordersAPI.handlePatchAccount)
		account.DELETE("/:id", a.ordersAPI.handleDeleteAccount)
		account.DELETE("/:id/hard", a.ordersAPI.handleHardDeleteAccount)
		account.POST("/merge", a.ordersAPI.handleMergeAccounts)
	}
	baseV2Path.GET("/accounts", a.ordersAPI.handleFetchAccounts)

	order := baseV2Path.Group("/order")
	{
		order.GET("/:id", a.ordersAPI.handleOrderGetByID)
		order.DELETE("/:id", a.ordersAPI.handleOrderDeleteByID)
		order.POST("/", a.ordersAPI.handleV2OrderCreate)
		order.POST("/update_token", a.ordersAPI.handleOrdersUpdateToken)
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
		transaction.POST("/new_token", a.ordersAPI.handleTransactionNewToken)
		transaction.POST("/new_token_no_cvv", a.ordersAPI.handleTransactionNewTokenNoCVV)
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
		special.DELETE("/:id", a.ordersAPI.handleSpecialDeleteById)
		special.DELETE("/delete/:keycloak_id", a.ordersAPI.handleDeleteSpecialsByKeycloakId)
		special.POST("/", a.ordersAPI.handleCreateSpecial)
		special.POST("/update", a.ordersAPI.handleSpecialUpdateKeycloakIdByEmail)
		special.GET("/email/:email", a.ordersAPI.handleSpecialGetByEmail)
		special.GET("/id/:id", a.ordersAPI.handleSpecialGetById)
		special.GET("/keycloak_id/:keycloak_id", a.ordersAPI.handleSpecialGetByKeycloakId)
		special.GET("/", a.ordersAPI.handleSpecialGetAll)
	}

	manualDiscount := baseV2Path.Group("/manual_discount")
	{
		manualDiscount.POST("/", a.ordersAPI.handleCreateOrUpdateManualDiscount)
		manualDiscount.DELETE("/:keycloak_id", a.ordersAPI.handleCancelManualDiscount)
		manualDiscount.GET("/", a.ordersAPI.handleGetAllManualDiscounts)
	}

	baseV2Path.DELETE("/hh_grant/:keycloak_id", a.ordersAPI.handleCancelHHGrant)

	hhRequest := baseV2Path.Group("/hh_request")
	{
		hhRequest.POST("/", a.ordersAPI.handleCreateHHRequest)
		hhRequest.GET("/", a.ordersAPI.handleGetAllHHRequests)
		hhRequest.POST("/:id/conclude", a.ordersAPI.handleConcludeHHRequest)
	}

	operation := baseV2Path.Group("/operation")
	{
		operation.POST("/", a.ordersAPI.handleOperationCreate)
		operation.POST("/revert", a.ordersAPI.handleOperationRevert)
	}

	pricing := baseV2Path.Group("/pricing")
	{
		pricing.GET("/monthly/:keycloak_id", a.ordersAPI.handleMonthlyPriceByKCID)
	}

	couponGroup := baseV2Path.Group("/coupon")
	{
		couponGroup.POST("/", a.couponAPI.handleCreateCoupon)
		couponGroup.GET("/", a.couponAPI.handleListCoupons)
		couponGroup.GET("/mine", a.couponAPI.handleGetMyCoupons)
		couponGroup.POST("/redeem", a.couponAPI.handleRedeemCoupon)
		couponGroup.GET("/:id/redemptions", a.couponAPI.handleGetCouponRedemptions)
		couponGroup.PATCH("/:id", a.couponAPI.handleUpdateCoupon)
		couponGroup.DELETE("/:id/redemption/:rid", a.couponAPI.handleRevokeRedemption)
	}

	a.gEngine.GET("/status/:email", a.ordersAPI.status)
}

func (a *App) initHealth() {
	h, _ := health.New(health.WithComponent(health.Component{
		Name:    common.ServiceName,
		Version: common.GitSHA,
	}), health.WithChecks(
		health.Config{
			Name:    "postgres",
			Timeout: time.Second * 5,
			Check:   healthpgx.New(healthpgx.Config{DSN: repo.GetDBURL()}),
		},
	))
	if common.Config.NatsUrl != "" {
		h.Register(health.Config{
			Name:    "nats",
			Timeout: time.Second * 5,
			Check:   healthnats.New(healthnats.Config{DSN: common.Config.NatsUrl}),
		})
	}

	a.gEngine.GET("/health", func(c *gin.Context) {
		h.HandlerFunc(c.Writer, c.Request)
	})
}

// Run serves until ctx is done, then returns so its caller's deferred Shutdown
// can drain.
func (a *App) Run(ctx context.Context) {
	server := &http.Server{
		Addr:    ":" + common.Config.Port,
		Handler: a.gEngine,
	}
	a.serve(ctx, server, server.ListenAndServe)
}

// serve is Run with the listen call injected, so a test can make the bind fail
// after a signal has already sent it down the graceful path.
func (a *App) serve(ctx context.Context, server *http.Server, listen func() error) {
	listenErr := make(chan error, 1)
	go func() {
		if err := listen(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
	}()
	slog.Info("listening", slog.String("addr", server.Addr))

	// The listen failure is handled here rather than in the goroutine, so the
	// exit belongs to one goroutine. Calling FatalAfter from there raced this
	// one: the drain takes seconds, and a signal arriving inside that window let
	// Run return, serverFn's deferred Shutdown find the Once already taken, and
	// main exit 0 — reporting success for a server that never bound its port.
	select {
	case err := <-listenErr:
		a.fatal("http.ListenAndServe", slog.Any("err", err))
	case <-ctx.Done():
		// Before the 15s request grace, which is the longest wait in the exit.
		// The fatal paths get this from a.fatal; this one returns, so it does it
		// here.
		if a.stop != nil {
			a.stop()
		}
		slog.Info("signal received, shutting down")
		a.stopServer(server, shutdownGrace)

		a.reportLateListenError(listenErr)
	}
}

// fatal reports why the process is dying, drains, and exits.
func (a *App) fatal(msg string, args ...any) {
	if a.stop != nil {
		a.stop()
	}

	utils.FatalAfter(a.Shutdown, msg, args...)
}

// reportLateListenError exits non-zero if the server had in fact failed to
// listen, after a signal has already sent Run down the graceful path.
func (a *App) reportLateListenError(listenErr <-chan error) {
	select {
	case err := <-listenErr:
		a.fatal("http.ListenAndServe", slog.Any("err", err))
	case <-time.After(listenErrorWindow):
	}
}

// stopServer stops serving, waits grace for in-flight requests, and then takes
// the connections out from under whatever is left.
func (a *App) stopServer(server *http.Server, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	err := server.Shutdown(ctx)
	if err == nil {
		return
	}

	slog.Error("http.Server.Shutdown: requests still in flight after the grace, closing their connections",
		slog.Any("err", err), slog.Duration("grace", grace))
	sentry.CaptureException(err)

	if err := server.Close(); err != nil {
		slog.Error("http.Server.Close", slog.Any("err", err))
	}
}

// shutdownGrace bounds how long in-flight requests have once a signal arrives.
const shutdownGrace = 15 * time.Second

// listenErrorWindow is how long Run looks for a listen failure after a signal
// has already sent it down the graceful path.
const listenErrorWindow = 250 * time.Millisecond

// Shutdown drains what the app owns: the event listener, the connection pool,
// the event emitter, then Sentry. It runs once, and survives a partial
// Initialize, since the fatal paths call it too.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(a.shutdown)
}

func (a *App) shutdown() {
	// Each step has its own bound, so a stuck pgxpool.Close (it waits for every
	// acquired connection) cannot eat the emitter's grace.
	if a.eventListener != nil {
		runBounded("event listener", profiles.CloseGrace+stepSlack, a.eventListener.Close)
	}
	closeRepo := a.repoClose
	if closeRepo == nil && a.repo != nil {
		closeRepo = a.repo.Close
	}
	if closeRepo != nil {
		runBounded("repo", poolCloseGrace, closeRepo)
	}
	if a.eventEmitter != nil {
		runBounded("event emitter", emitterDrainGrace+stepSlack, func() {
			ctx, cancel := context.WithTimeout(context.Background(), emitterDrainGrace)
			defer cancel()
			a.eventEmitter.Close(ctx)
		})
	}

	sentry.Flush(sentryFlushGrace)
}

// runBounded gives step its own grace and reports an overshoot. Returning
// matters more than finishing: what follows is another step that also has data
// to flush, or the exit.
func runBounded(what string, grace time.Duration, step func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		step()
	}()

	select {
	case <-done:
	case <-time.After(grace):
		slog.Error("App.Shutdown step did not finish, continuing",
			slog.String("step", what), slog.Duration("grace", grace))
		sentry.CaptureMessage("App.Shutdown step timed out: " + what)
	}
}

// The grants each shutdown step gets, and the budget they add up to.
const (
	// The emitter's whole close, not just its drain timeout: the library adds a
	// hardcoded 5s flush after the subscription phase, and losing that race
	// costs a spurious Sentry failure as well as the publishes.
	emitterDrainGrace = events.CloseBudget
	poolCloseGrace    = 3 * time.Second
	sentryFlushGrace  = 2 * time.Second

	// Outer grace = inner deadline + stepSlack, so a step that uses its whole
	// deadline is not reported as hung.
	stepSlack = time.Second

	shutdownBudget = (profiles.CloseGrace + stepSlack) + poolCloseGrace + (emitterDrainGrace + stepSlack)
)

// stopGracePeriod is how long the orchestrator waits before SIGKILL, and the
// whole exit has to fit inside it: this much for in-flight requests, then the
// budget above, then the Sentry flush.
const stopGracePeriod = 45 * time.Second

// Guarded at compile time as well: subtracting on unsigned constants makes an
// edit that overruns a build failure.
const _ = uint64(stopGracePeriod - shutdownGrace - shutdownBudget - sentryFlushGrace)

func (a *App) SetEmitter(emitter events.EventEmitter) {
	a.eventEmitter = emitter
}
