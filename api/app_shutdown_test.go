package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// Shutdown is reached from the fatal paths as well as from server.go's defer, so
// it runs with whatever Initialize managed to build. A fatal in initSentry or
// initEventEmitter leaves both fields nil, and an unguarded Close on a nil
// interface panics — turning a stated error into a crash.
func TestShutdownSurvivesAPartialInitialize(t *testing.T) {
	new(App).Shutdown()
}

// The emitter is always built. It used to be built only when NatsUrl was set,
// which left a nil interface that the first emitEvent would call Emit on —
// reachable by following .env_example, which ships NATS_URL empty.
func TestTheEmitterIsBuiltWithoutNats(t *testing.T) {
	saved := *common.Config
	t.Cleanup(func() { *common.Config = saved })
	common.Config.NatsUrl = ""

	var app App
	app.initEventEmitter()

	if app.eventEmitter == nil {
		t.Fatal("no emitter without NATS: the first emitEvent would panic on a nil interface")
	}
}

// The listener is reached from the fatal paths too, including the one where its
// own Run failed, so Shutdown must survive a listener that never started.
func TestShutdownSurvivesAListenerThatNeverRan(t *testing.T) {
	app := App{eventListener: new(profiles.EventListener)}
	app.Shutdown()
}

// initDB must publish the repo only after the error check. NewOrdersDB returns a
// concrete *repo.OrdersDB, so assigning it first boxes a nil pointer into a
// non-nil interface: Shutdown's guard passes and Close panics on the nil
// receiver. A source check because initDB exits on failure, and because the
// hazard is the ordering rather than any value at run time.
func TestInitDBPublishesTheRepoOnlyOnSuccess(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(source, []byte("a.repo, err = repo.NewOrdersDB")) {
		t.Error("initDB assigns NewOrdersDB straight into the interface field: on failure that " +
			"is a typed nil, which defeats Shutdown's guard. Assign through a local and set " +
			"a.repo after the error check")
	}

	assign := bytes.Index(source, []byte("a.repo = ordersDB"))
	check := bytes.Index(source, []byte(`"connect to db"`))
	switch {
	case assign < 0:
		t.Error("initDB should assign a.repo from a local after checking the error")
	case assign < check:
		t.Error("a.repo is assigned before the error is checked")
	}
}

// Shutdown must stop the listener, and stop it before the repo. The consumer
// goroutine writes through the repo, so closing the pool first turns in-flight
// profile events into `closed pool` errors that are reported and never acked.
//
// A source check on the order, because the effect is only observable with a live
// JetStream consumer.
func TestShutdownStopsTheListenerBeforeTheRepo(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}

	body := bytes.Index(source, []byte("func (a *App) Shutdown()"))
	if body < 0 {
		t.Fatal("Shutdown not found in app.go")
	}
	shutdown := source[body:]

	listener := bytes.Index(shutdown, []byte("a.eventListener.Close()"))
	repo := bytes.Index(shutdown, []byte("a.repo.Close()"))
	switch {
	case listener < 0:
		t.Error("Shutdown must close the event listener: it holds a JetStream consumer and its " +
			"own NATS connection, and nothing else closes them")
	case repo < 0:
		t.Error("Shutdown should close the repo")
	case listener > repo:
		t.Error("Shutdown closes the repo before stopping the listener, so in-flight profile " +
			"events fail against a closed pool and are never acked")
	}
}

// Run has to return on SIGTERM rather than let the runtime kill the process,
// because its caller's deferred Shutdown is the only thing that drains. Driven
// in a child, since the test process cannot signal itself without ending the
// run.
func TestRunReturnsOnSigterm(t *testing.T) {
	if os.Getenv("RUN_SIGTERM_CHILD") == "1" {
		saved := *common.Config
		defer func() { *common.Config = saved }()
		common.Config.Port = freePort(t)

		app := App{gEngine: gin.New()}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer stop()
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()

		app.Run(ctx, stop)
		fmt.Fprintln(os.Stderr, "RUN RETURNED")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestRunReturnsOnSigterm", "-test.timeout=30s")
	child.Env = append(os.Environ(), "RUN_SIGTERM_CHILD=1")
	out, err := child.CombinedOutput()
	output := string(out)

	// Told apart from the SIGTERM question, so a busy port does not read as
	// broken signal handling.
	if strings.Contains(output, "http.ListenAndServe") {
		t.Fatalf("the child could not listen, so this test proved nothing:\n%s", output)
	}
	if !strings.Contains(output, "RUN RETURNED") {
		t.Errorf("Run did not return on SIGTERM (child: %v), so Shutdown would never drain:\n%s",
			err, output)
	}
}

// freePort asks the kernel for a port and hands it back. Racy in principle,
// unlike a hard-coded port which fails whenever CI happens to hold it.
func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// stuckRepo stands in for a pgx pool whose Close blocks — a handler parked on a
// row lock holds an acquired connection, and pgxpool.Close waits for it.
type stuckRepo struct {
	repo.OrdersRepository
	release chan struct{}
}

func (r *stuckRepo) Close() { <-r.release }

// Shutdown has to be bounded as a whole. Bounding only the listener's wait moved
// the hang one line down: the repo close blocks, so the emitter is never drained
// and, on a fatal path, os.Exit is never reached.
func TestShutdownGivesUpOnAStuckClose(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	app := App{repo: &stuckRepo{release: release}}

	start := time.Now()
	app.Shutdown()
	waited := time.Since(start)

	if waited > shutdownBudget+3*time.Second {
		t.Fatalf("Shutdown waited %v on a stuck close", waited)
	}
	if waited < shutdownBudget {
		t.Fatalf("Shutdown returned after %v, before its own budget — nothing is being waited on", waited)
	}
}

// Shutdown runs once however many times it is called. A bind failure taking the
// FatalAfter path while SIGTERM makes Run return had both callers draining at
// the same time, which closes the emitter twice and reports two failures that
// did not happen.
func TestShutdownRunsOnce(t *testing.T) {
	var closes atomic.Int64
	release := make(chan struct{})
	close(release)
	app := App{repo: &countingRepo{release: release, closes: &closes}}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			app.Shutdown()
		}()
	}
	wg.Wait()

	if closes.Load() != 1 {
		t.Errorf("the repo was closed %d times, want once", closes.Load())
	}
}

type countingRepo struct {
	repo.OrdersRepository
	release chan struct{}
	closes  *atomic.Int64
}

func (r *countingRepo) Close() { r.closes.Add(1); <-r.release }

// A request that outlasts the grace gets its connection closed, which cancels
// its context — so a handler that honours the context unwinds instead of running
// on into a pool that Shutdown is about to close.
func TestStopServerCancelsRequestsThatOutlastTheGrace(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	})

	port := freePort(t)
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: handler}
	go func() { _ = server.ListenAndServe() }()

	go func() {
		resp, err := http.Get("http://127.0.0.1:" + port + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the handler")
	}

	var app App
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.stopServer(server, 50*time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stopServer did not return: it waited for a handler that never finishes")
	}

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the request context was never cancelled, so the handler runs on into a closed pool")
	}
}

// The budget has to cover every wait nested inside it, with something left for
// the pool close between them. It used to be exactly the sum of the two waits,
// so pgxpool.Close spent the emitter's drain and the budget expired before the
// The whole exit has to fit the grace the deployment grants. Also guarded at
// compile time in app.go; this states it in a form that names the numbers when
// it fails.
//
// What is deliberately *not* asserted here is that shutdownBudget covers the
// waits inside it. It is derived from those same constants, so any such
// comparison is arithmetic on itself and cannot fail — an earlier version of
// this test made exactly that comparison and stayed green through round 14's
// undercount. The property it was reaching for, that profiles.CloseGrace bounds
// EventListener.Close as a whole however many waits it makes, is tested in
// pkg/profiles where it can be observed: TestCloseIsBoundedAsAWhole.
func TestTheExitFitsTheGraceItIsGiven(t *testing.T) {
	needed := shutdownGrace + shutdownBudget + sentryFlushGrace

	if needed >= stopGracePeriod {
		t.Errorf("the exit needs %v (requests %v + drain %v + flush %v) against a %v grace",
			needed, shutdownGrace, shutdownBudget, sentryFlushGrace, stopGracePeriod)
	}
}

// A server that cannot bind must exit non-zero. The failure used to be handled
// on a goroutine while main waited for a signal, so a signal arriving during the
// drain let main return first and the process reported success.
func TestRunExitsNonZeroWhenItCannotBind(t *testing.T) {
	if os.Getenv("RUN_BIND_CHILD") == "1" {
		saved := *common.Config
		defer func() { *common.Config = saved }()

		// Hold the port, so the App cannot have it.
		port := freePort(t)
		blocker, err := net.Listen("tcp", ":"+port)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Close()

		common.Config.Port = port
		app := App{gEngine: gin.New()}
		app.Run(context.Background(), func() {})
		fmt.Fprintln(os.Stderr, "RUN RETURNED")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestRunExitsNonZeroWhenItCannotBind", "-test.timeout=30s")
	child.Env = append(os.Environ(), "RUN_BIND_CHILD=1")
	out, err := child.CombinedOutput()
	output := string(out)

	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("a server that cannot bind must exit non-zero, got %v:\n%s", err, output)
	}
	if exit.ExitCode() != 1 {
		t.Errorf("exit code %d, want 1:\n%s", exit.ExitCode(), output)
	}
	if !strings.Contains(output, "http.ListenAndServe") {
		t.Errorf("the bind failure was not reported:\n%s", output)
	}
}

// The payment handlers must post to checkout with the request's values but not
// its cancellation: they write payment rows first, so a caller that goes away
// mid-call must not abandon a call that may already have charged.
//
// A source check because the alternative is a live checkout: what matters is
// which context reaches PostJSON.
func TestThePaymentPostsAreNotCancelledByTheCaller(t *testing.T) {
	source, err := os.ReadFile("transaction_handler.go")
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(source, []byte("utils.PostJSON(c.Request.Context()")) {
		t.Error("a payment POST takes the request context directly: a caller that disconnects " +
			"then cancels a call that may already have charged, with rows already written. " +
			"Use paymentCallContext, which strips cancellation and keeps the values")
	}
	if !bytes.Contains(source, []byte("context.WithoutCancel(c.Request.Context())")) {
		t.Error("paymentCallContext should build on context.WithoutCancel")
	}
}

// The grace the code budgets against has to be the grace the deployment
// actually grants. It used to be neither: the constant claimed 30s because
// "a container gets 30s by default", which is Kubernetes' default, while this
// service deploys with docker compose — whose default is 10s, less than
// shutdownGrace alone. So SIGKILL landed mid-drain on every deploy with a
// request in flight, with the compile guard green.
func TestTheExitFitsTheDeclaredStopGracePeriod(t *testing.T) {
	compose, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}

	declared := regexp.MustCompile(`stop_grace_period:\s*(\S+)`).FindSubmatch(compose)
	if declared == nil {
		t.Fatal("docker-compose.yml declares no stop_grace_period, so Compose's 10s default " +
			"applies — less than shutdownGrace alone")
	}

	granted, err := time.ParseDuration(string(declared[1]))
	if err != nil {
		t.Fatalf("stop_grace_period %q does not parse: %v", declared[1], err)
	}

	if granted != stopGracePeriod {
		t.Errorf("docker-compose.yml grants %v, the code budgets against %v", granted, stopGracePeriod)
	}

	needed := shutdownGrace + shutdownBudget + sentryFlushGrace
	if needed >= granted {
		t.Errorf("the exit needs %v and the deployment grants %v", needed, granted)
	}
}

// Initialize has to observe the signal, because startup is not instant:
// migrations and the JWKS fetch both wait on something, and a signal arriving
// then used to be noticed only once Run began.
func TestInitializeStopsOnAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := new(App).Initialize(ctx)

	if err == nil {
		t.Fatal("Initialize ran to completion on a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
}

// serverFn must defer the drain before it starts building, or a failure
// part-way through startup drains nothing, and must restore the default signal
// disposition before the drain rather than after it — otherwise a second
// Ctrl-C during a slow drain is swallowed.
func TestServerFnOrdersItsShutdownAndSignalRestore(t *testing.T) {
	source, err := os.ReadFile("../cmd/server.go")
	if err != nil {
		t.Fatal(err)
	}

	deferShutdown := bytes.Index(source, []byte("defer app.Shutdown()"))
	// Matched on the call, not the argument list: these checks have broken twice
	// on signature changes that left the ordering they protect intact.
	initialize := bytes.Index(source, []byte("app.Initialize("))
	run := bytes.Index(source, []byte("app.Run("))
	stop := bytes.LastIndex(source, []byte("stop()"))

	switch {
	case deferShutdown < 0 || initialize < 0 || run < 0:
		t.Fatal("serverFn does not look like it did: check this test before the code")
	case deferShutdown > initialize:
		t.Error("the drain is deferred after Initialize, so a failure during startup drains nothing")
	case stop < run:
		t.Error("the signal disposition is restored before Run returns, so the shutdown it " +
			"triggers cannot be interrupted by a second signal")
	}
}

// A signal during startup is a stop, not a crash. serverFn has to tell the two
// apart, because Initialize reports both as an error: reported as a failure it
// would be `docker compose up -d` recreating the container mid-migration, and
// the old process exiting 1 for having been asked to stop.
func TestServerFnTreatsAStartupSignalAsACleanStop(t *testing.T) {
	source, err := os.ReadFile("../cmd/server.go")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(source, []byte("errors.Is(err, context.Canceled)")) {
		t.Error("serverFn does not distinguish a cancelled startup from a failed one, so a " +
			"signal during Initialize exits 1")
	}

	initErr := bytes.Index(source, []byte("app.Initialize(ctx); err != nil"))
	fatal := bytes.Index(source, []byte(`utils.FatalAfter(app.Shutdown, "app.Initialize"`))
	stop := bytes.Index(source, []byte("stop()"))
	switch {
	case initErr < 0 || fatal < 0:
		t.Fatal("serverFn does not look like it did: check this test before the code")
	case stop > fatal:
		t.Error("the signal disposition is restored after the fatal drain, so a second signal " +
			"cannot cut that drain short either")
	}
}

// A listen failure that arrives after a signal has taken Run down the graceful
// path must still exit non-zero. Reproducing the race itself is not reliable —
// either branch of Run's select can win — so this drives the reporting
// directly, in a child process because it ends in os.Exit.
func TestALateListenErrorStillExitsNonZero(t *testing.T) {
	if os.Getenv("LATE_LISTEN_CHILD") == "1" {
		listenErr := make(chan error, 1)
		listenErr <- errors.New("listen tcp :8185: bind: address already in use")

		new(App).reportLateListenError(listenErr, func() {})
		fmt.Fprintln(os.Stderr, "RETURNED INSTEAD OF EXITING")
		return
	}
	if os.Getenv("LATE_LISTEN_SLOW_CHILD") == "1" {
		// Arrives after the call starts, which is the case a non-blocking check
		// misses: with a signal already pending, Run can reach this before the
		// listener has tried to bind.
		listenErr := make(chan error, 1)
		go func() {
			time.Sleep(50 * time.Millisecond)
			listenErr <- errors.New("listen tcp :8185: bind: address already in use")
		}()

		new(App).reportLateListenError(listenErr, func() {})
		fmt.Fprintln(os.Stderr, "RETURNED INSTEAD OF EXITING")
		return
	}
	if os.Getenv("LATE_LISTEN_QUIET_CHILD") == "1" {
		new(App).reportLateListenError(make(chan error, 1), func() {})
		fmt.Fprintln(os.Stderr, "RETURNED")
		return
	}

	loud := exec.Command(os.Args[0], "-test.run=TestALateListenErrorStillExitsNonZero")
	loud.Env = append(os.Environ(), "LATE_LISTEN_CHILD=1")
	out, err := loud.CombinedOutput()
	output := string(out)

	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Errorf("a server that never bound must exit 1, got %v:\n%s", err, output)
	}
	if !strings.Contains(output, "http.ListenAndServe") {
		t.Errorf("the failure was not reported:\n%s", output)
	}
	if strings.Contains(output, "RETURNED INSTEAD OF EXITING") {
		t.Errorf("it returned instead of exiting:\n%s", output)
	}

	// The same, but arriving just after the call begins — which is what the
	// window is for, and what a non-blocking check cannot see.
	slow := exec.Command(os.Args[0], "-test.run=TestALateListenErrorStillExitsNonZero")
	slow.Env = append(os.Environ(), "LATE_LISTEN_SLOW_CHILD=1")
	slowOut, slowErr := slow.CombinedOutput()
	if exit, ok := slowErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Errorf("a failure arriving during the window must still exit 1, got %v:\n%s",
			slowErr, slowOut)
	}

	// And a shutdown with no listen failure must not be delayed into a fatal.
	quiet := exec.Command(os.Args[0], "-test.run=TestALateListenErrorStillExitsNonZero")
	quiet.Env = append(os.Environ(), "LATE_LISTEN_QUIET_CHILD=1")
	quietOut, quietErr := quiet.CombinedOutput()
	if quietErr != nil {
		t.Errorf("a clean shutdown must not exit non-zero: %v\n%s", quietErr, quietOut)
	}
	if !strings.Contains(string(quietOut), "RETURNED") {
		t.Errorf("it did not return on a clean shutdown:\n%s", quietOut)
	}
}

// Run has to consult that channel on the graceful path. Testing the reporting
// alone leaves the call site unpinned, and the call site is the whole point.
func TestRunChecksForALateListenErrorOnTheGracefulPath(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}

	body := regexp.MustCompile(`(?s)func \(a \*App\) Run\([^\n]*\) \{(.*?)\n\}`).
		FindSubmatch(source)
	if body == nil {
		t.Fatal("Run was not found in app.go")
	}

	graceful := bytes.Index(body[1], []byte("case <-ctx.Done():"))
	report := bytes.Index(body[1], []byte("a.reportLateListenError("))
	switch {
	case report < 0:
		t.Error("Run does not check for a listen failure after the graceful path, so a signal " +
			"racing a bind failure exits 0")
	case report < graceful:
		t.Error("the check runs before the graceful branch, where it cannot see a failure that " +
			"arrives during the shutdown")
	}
}

// Run's fatal paths must restore the default signal disposition before they
// drain. serverFn does it when Run returns, and these paths never return — so
// without it the drain they start swallows every later signal, which is the
// escape hatch Run's doc promises.
func TestRunRestoresTheSignalDispositionBeforeItsFatalDrains(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}

	// Prefixes, so a changed parameter list is not mistaken for a missing
	// function: that has already turned this class of check red twice.
	for _, fn := range []struct{ name, signature string }{
		{"Run", `func (a *App) Run(`},
		{"reportLateListenError", `func (a *App) reportLateListenError(`},
	} {
		at := bytes.Index(source, []byte(fn.signature))
		if at < 0 {
			t.Fatalf("%s not found: check this test before the code", fn.name)
		}

		body := source[at:]
		if next := bytes.Index(body[len(fn.signature):], []byte("\nfunc ")); next >= 0 {
			body = body[:len(fn.signature)+next]
		}

		fatal := bytes.Index(body, []byte("utils.FatalAfter(a.Shutdown"))
		if fatal < 0 {
			continue
		}
		if restore := bytes.LastIndex(body[:fatal], []byte("stop()")); restore < 0 {
			t.Errorf("%s drains without restoring the signal disposition first, so a second "+
				"signal cannot interrupt it", fn.name)
		}
	}
}
