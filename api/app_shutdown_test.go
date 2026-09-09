package api

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
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
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()

		app.Run()
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
// emitter had any of it.
func TestTheShutdownBudgetCoversWhatIsInsideIt(t *testing.T) {
	nested := profiles.DrainGrace + emitterDrainGrace

	if shutdownBudget <= nested {
		t.Errorf("budget %v does not exceed the waits inside it (%v + %v): the pool close between "+
			"them takes its time out of the emitter drain",
			shutdownBudget, profiles.DrainGrace, emitterDrainGrace)
	}

	// And the whole exit still has to fit a container's grace period. Also
	// guarded at compile time in app.go; this says it in a form that names the
	// budget when it fails.
	total := shutdownGrace + shutdownBudget + sentryFlushGrace
	if total >= sigkillAfter {
		t.Errorf("the exit needs %v (requests %v + drain %v + flush %v) against a %v SIGKILL",
			total, shutdownGrace, shutdownBudget, sentryFlushGrace, sigkillAfter)
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
		app.Run()
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
