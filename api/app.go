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
	healthpgx "github.com/hellofresh/health-go/v5/checks/pgx4"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type App struct {
	repo                repo.OrdersRepository
	eventEmitter        events.EventEmitter
	eventListener       *profiles.EventListener
	domainEventsHandler *domain.EventsHandler
	ordersAPI           *OrdersAPI
	gEngine             *gin.Engine

	shutdownOnce sync.Once
}

func NewApp() *App {
	return new(App)
}

// Initialize builds the app, stopping early if the process is already being
// asked to shut down.
//
// Startup is not instant — migrations and the JWKS fetch both talk to something
// — and a signal arriving during it used to be noticed only once Run began. The
// steps are cheap to abandon: nothing here is a payment, and the caller's
// deferred Shutdown drains whatever was built.
func (a *App) Initialize(ctx context.Context) error {
	steps := []struct {
		name string
		run  func()
	}{
		{"sentry", a.initSentry},
		{"events emitter", a.initEventEmitter},
		{"database", a.initDB},
		{"events listener", a.initEventListener},
		{"api", func() { a.ordersAPI = NewOrdersAPI(a.repo) }},
		{"gin engine", a.initGinEngine},
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

// initEventEmitter always builds one. It used to build one only when NatsUrl was
// set, which left the field a nil interface — and nothing tolerates that: the
// first emitEvent calls Emit on it and panics, and so does Shutdown. An empty
// NATS_URL is the documented default in .env_example, so that was reachable by
// following the instructions.
//
// events.CreateEmitter already degrades to logging-only without NATS, which is
// what the condition was reaching for.
func (a *App) initEventEmitter() {
	slog.Info("initializing events emitter")

	var err error
	a.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		utils.FatalAfter(a.Shutdown, "events.CreateEmitter", slog.Any("err", err))
	}
}

func (a *App) initDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Through a local: NewOrdersDB returns a concrete *repo.OrdersDB, and on
	// failure that nil pointer boxes into a non-nil interface — Shutdown's guard
	// would pass and its Close would panic on the nil receiver.
	ordersDB, err := repo.NewOrdersDB(ctx, a.eventEmitter)
	if err != nil {
		utils.FatalAfter(a.Shutdown, "connect to db", slog.Any("err", err))
	}
	a.repo = ordersDB

	err = repo.SyncDBStructInsertionAndMigrations()
	if err != nil {
		utils.FatalAfter(a.Shutdown, "db migrations", slog.Any("err", err))
	}

	slog.Info("db connected and migrated")
}

func (a *App) initEventListener() {
	if common.Config.NatsUrl != "" {
		slog.Info("initializing events listener")

		var err error
		a.eventListener, err = profiles.NewEventListener()
		if err != nil {
			utils.FatalAfter(a.Shutdown, "profiles.NewEventListener", slog.Any("err", err))
		}

		a.domainEventsHandler = domain.NewEventsHandler(a.repo)
		a.eventListener.RegisterHandler(a.domainEventsHandler.HandleProfilesEvent)

		if err = a.eventListener.Run(); err != nil {
			utils.FatalAfter(a.Shutdown, "eventListener.Run", slog.Any("err", err))
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

func (a *App) initGinEngine() {
	gin.SetMode(common.Config.Mode)
	a.gEngine = gin.New()
	issuerUrl := fmt.Sprintf("%s/auth/realms/%s", common.Config.KeycloakServerUrl, common.Config.KeycloakRealm)
	tokenVerifier, err := middleware.NewFailoverOIDCTokenVerifier(issuerUrl)
	if err != nil {
		utils.FatalAfter(a.Shutdown, "middleware.NewFailoverOIDCTokenVerifier", slog.Any("err", err))
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
		middleware.Authentication(tokenVerifier),
	)

	a.initRoutes()
}

func (a *App) initRoutes() {
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
		couponGroup.POST("/", a.ordersAPI.handleCreateCoupon)
		couponGroup.GET("/", a.ordersAPI.handleListCoupons)
		couponGroup.GET("/mine", a.ordersAPI.handleGetMyCoupons)
		couponGroup.POST("/redeem", a.ordersAPI.handleRedeemCoupon)
		couponGroup.GET("/:id/redemptions", a.ordersAPI.handleGetCouponRedemptions)
		couponGroup.PATCH("/:id", a.ordersAPI.handleUpdateCoupon)
		couponGroup.DELETE("/:id/redemption/:rid", a.ordersAPI.handleRevokeRedemption)
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
//
// The signal is not registered here. It used to be, which left everything
// before Run — migrations, the JWKS fetch — running under the default
// disposition: a SIGTERM during startup killed the process outright, with NATS
// already connected and nothing drained. serverFn registers it before
// Initialize and hands the context down, so a signal arriving during startup
// means Run returns at once and the drain still happens.
//
// It used to call (*gin.Engine).Run, which returns only on a listen error, so
// the only exit it covered was a failure to bind.
//
// In-flight requests get shutdownGrace to finish; past that their connections
// are closed.
//
// stop is the signal registration's undo, and Run needs it rather than leaving
// it to its caller: serverFn restores the disposition when Run returns, and the
// fatal paths here never return. Without it the drain they start swallows every
// later signal, which is the escape hatch this doc used to promise and not
// provide.
func (a *App) Run(ctx context.Context, stop func()) {
	server := &http.Server{
		Addr:    ":" + common.Config.Port,
		Handler: a.gEngine,
	}

	listenErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
		// Signals back to their default disposition before the drain, so a
		// second one can cut it short. serverFn does this once Run returns, and
		// these paths never return.
		stop()
		utils.FatalAfter(a.Shutdown, "http.ListenAndServe", slog.Any("err", err))
	case <-ctx.Done():
		slog.Info("signal received, shutting down")
		a.stopServer(server, shutdownGrace)

		a.reportLateListenError(listenErr, stop)
	}
}

// reportLateListenError exits non-zero if the server had in fact failed to
// listen, after a signal has already sent Run down the graceful path.
//
// Both cases of Run's select can be ready at once — a signal arriving as the
// listener fails — and select picks either. Taking the graceful one left the
// failure unread in its buffered channel and the process exiting 0 for a server
// that never bound.
//
// Waited for rather than polled: with a signal already pending, that branch can
// be reached before the goroutine has tried to bind at all, so a non-blocking
// check finds nothing. A bind fails immediately when it fails, so this is long
// enough to tell the two apart and short enough to be invisible in a real
// shutdown.
func (a *App) reportLateListenError(listenErr <-chan error, stop func()) {
	select {
	case err := <-listenErr:
		stop()
		utils.FatalAfter(a.Shutdown, "http.ListenAndServe", slog.Any("err", err))
	case <-time.After(listenErrorWindow):
	}
}

// stopServer stops serving, waits grace for in-flight requests, and then takes
// the connections out from under whatever is left.
//
// http.Server.Shutdown does not cancel handlers, it stops waiting for them, so
// on its own it leaves them running into a pool that App.Shutdown is about to
// close — the same failure the listener ordering exists to prevent, arriving
// from the HTTP side. Close is the part that ends it: dropping a connection
// cancels that request's context, so a handler that honours it unwinds, and the
// pgx calls underneath it are cancelled with it.
//
// A request that ignores its context still runs, and will now fail against a
// closed pool. That is deliberate: it has had its grace, and the alternative is
// a shutdown that never finishes.
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
//
// Back at 15s: it was cut to 12s to make the compile guard below pass, which is
// tuning a safety margin to fit arithmetic. The derived budget leaves room for
// 15s, and the requests that need it are the ones that post to checkout.
const shutdownGrace = 15 * time.Second

// listenErrorWindow is how long Run looks for a listen failure after a signal
// has already sent it down the graceful path.
const listenErrorWindow = 250 * time.Millisecond

// Shutdown is called from the fatal paths as well as from server.go's defer, so
// it has to survive a partial Initialize: either field can still be nil when a
// fatal happens on the way up.
// Shutdown drains what the app owns, once and within a budget.
//
// Once, because it is reached from two directions: FatalAfter on the way out of
// a fatal, and serverFn's defer when Run returns. A bind failure racing a
// SIGTERM ran both at the same time, which closed the emitter twice — the
// second Drain reports ErrConnectionClosed as a failure, and only one of the two
// waiters can take the single ncClosed token, so the other spends its whole
// context and reports a second failure that never happened.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(a.shutdown)
}

func (a *App) shutdown() {
	// Bounded as a whole, not step by step. Every step here can block for as
	// long as something else holds it: pgxpool.Close waits for every acquired
	// connection to come back, so a handler parked on a row lock stops the
	// emitter from ever being drained — and on a fatal path, stops os.Exit from
	// being reached at all. Bounding only the listener's wait, as the previous
	// version did, moved the hang one line down.
	done := make(chan struct{})
	go func() {
		defer close(done)

		// The listener first: its runner writes through the repo, so closing
		// the pool underneath it turns in-flight profile events into
		// `closed pool` errors, reported to Sentry and never acked.
		if a.eventListener != nil {
			a.eventListener.Close()
		}
		if a.repo != nil {
			a.repo.Close()
		}
		if a.eventEmitter != nil {
			ctx, cancel := context.WithTimeout(context.Background(), emitterDrainGrace)
			defer cancel()
			a.eventEmitter.Close(ctx)
		}
	}()

	select {
	case <-done:
	case <-time.After(shutdownBudget):
		slog.Error("App.Shutdown did not finish in time, exiting anyway",
			slog.Duration("budget", shutdownBudget))
		sentry.CaptureMessage("App.Shutdown timed out")
	}

	sentry.Flush(sentryFlushGrace)
}

// The shutdown budget and the grants inside it.
//
// Derived from what each step is allowed, because picking it by hand went wrong
// twice: first at 10s against 5s for the listener and 5s for the emitter, which
// left the pool close between them with nothing; then again when the listener
// grew a second wait and kept spending a per-wait grant the budget funded once.
// profiles.CloseGrace is now the whole of Close, however many waits it makes. Any time pgxpool.Close spent came out of the
// emitter drain — the one step with data in it — and the budget expired before
// it started.
//
// poolCloseGrace is not enforceable: pgxpool.Close blocks until every acquired
// connection is returned and takes no context. It is an allowance in the budget,
// not a deadline on the call.
const (
	emitterDrainGrace = 4 * time.Second
	poolCloseGrace    = 3 * time.Second
	sentryFlushGrace  = 2 * time.Second

	shutdownBudget = profiles.CloseGrace + poolCloseGrace + emitterDrainGrace
)

// stopGracePeriod is how long the orchestrator waits before SIGKILL, and the
// whole exit has to fit inside it: this much for in-flight requests, then the
// budget above, then the Sentry flush.
//
// It is declared in docker-compose.yml as stop_grace_period, and it has to be:
// Compose's default is 10 seconds, which the ladder below overruns nearly
// threefold. This constant used to say 30s on the grounds that "a container
// gets 30s by default" — true of Kubernetes, which this service does not
// deploy to. Nothing was enforcing anything; the guard was checking arithmetic
// against another platform's number while SIGKILL landed mid-drain on every
// deploy with a request in flight.
//
// TestTheExitFitsTheDeclaredStopGracePeriod reads the Compose file, so the two
// cannot drift apart silently.
const stopGracePeriod = 35 * time.Second

// Guarded at compile time as well: subtracting on unsigned constants makes an
// edit that overruns a build failure.
const _ = uint64(stopGracePeriod - shutdownGrace - shutdownBudget - sentryFlushGrace)

func (a *App) SetEmitter(emitter events.EventEmitter) {
	a.eventEmitter = emitter
}
