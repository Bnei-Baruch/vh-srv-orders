package api

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
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
		common.Config.Port = "18099"

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
	out, _ := child.CombinedOutput()

	if !strings.Contains(string(out), "RUN RETURNED") {
		t.Errorf("Run did not return on SIGTERM, so Shutdown would never drain:\n%s", out)
	}
}
