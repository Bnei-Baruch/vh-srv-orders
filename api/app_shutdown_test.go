package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/profiles"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
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

// Shutdown must stop the listener, and stop it before the repo. The consumer
// goroutine writes through the repo, so closing the pool first turns in-flight
// profile events into `closed pool` errors that are reported and never acked.
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

	listener := bytes.Index(shutdown, []byte(`runBounded("event listener"`))
	repo := bytes.Index(shutdown, []byte(`runBounded("repo"`))
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

		app.Run(ctx)
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
	release chan struct{}
}

func (r *stuckRepo) Close() { <-r.release }

// stubEmitter records that the emitter was given its turn.
type stubEmitter struct{ closes atomic.Int64 }

func (e *stubEmitter) Emit(context.Context, ...events.Event) {}
func (e *stubEmitter) Close(context.Context)                 { e.closes.Add(1) }

// A step that hangs costs its own grace and nothing else's. Bounding the
// sequence instead meant a stuck pgxpool.Close — which waits for every acquired
// connection and takes no context — could spend the emitter's share, and the
// emitter is the only step here with data in it.
func TestAStuckStepDoesNotSpendTheNextStepsGrace(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	emitter := &stubEmitter{}
	app := App{repoClose: (&stuckRepo{release: release}).Close, eventEmitter: emitter}

	start := time.Now()
	app.Shutdown()
	waited := time.Since(start)

	if emitter.closes.Load() != 1 {
		t.Errorf("the emitter was closed %d times: a stuck repo close consumed its turn",
			emitter.closes.Load())
	}
	if waited < poolCloseGrace {
		t.Errorf("Shutdown returned after %v, before the repo's own grace of %v", waited, poolCloseGrace)
	}
	if waited > poolCloseGrace+emitterDrainGrace+3*time.Second {
		t.Errorf("Shutdown took %v: a stuck step is spending more than its grace", waited)
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
	app := App{repoClose: (&countingRepo{release: release, closes: &closes}).Close}

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
		app.Run(context.Background())
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

	err := new(App).Initialize(ctx, func() {})

	if err == nil {
		t.Fatal("Initialize ran to completion on a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
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

		new(App).reportLateListenError(listenErr)
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

		new(App).reportLateListenError(listenErr)
		fmt.Fprintln(os.Stderr, "RETURNED INSTEAD OF EXITING")
		return
	}
	if os.Getenv("LATE_LISTEN_QUIET_CHILD") == "1" {
		new(App).reportLateListenError(make(chan error, 1))
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
// alone leaves the call site unpinned, and the call site is the whole point:
// here the signal is already in when the bind fails, so serve takes the
// graceful branch and only that check turns the failure into exit 1.
func TestRunChecksForALateListenErrorOnTheGracefulPath(t *testing.T) {
	if os.Getenv("GRACEFUL_LISTEN_CHILD") == "1" {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		failLate := func() error {
			time.Sleep(50 * time.Millisecond)
			return errors.New("listen tcp :8185: bind: address already in use")
		}
		new(App).serve(ctx, &http.Server{}, failLate)
		fmt.Fprintln(os.Stderr, "RETURNED")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=^TestRunChecksForALateListenErrorOnTheGracefulPath$")
	child.Env = append(os.Environ(), "GRACEFUL_LISTEN_CHILD=1")
	out, err := child.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Errorf("a bind failure after the signal must exit 1, got %v — the graceful path does "+
			"not check for it:\n%s", err, out)
	}
	if strings.Contains(string(out), "RETURNED") {
		t.Errorf("serve returned, so main would exit 0 for a server that never listened:\n%s", out)
	}
}

// Every fatal in this type goes through a.fatal, which is what makes the drain
// and the signal restore impossible to half-apply. They have each been added to
// some paths and not others across three rounds — six sites in Initialize kept
// the signals trapped for a round after Run stopped doing so — and the guards
// written then were brittle in the other direction: one matched a `stop()` that
// belonged to a defer elsewhere, the other read only the branch it expected.
func TestEveryFatalPathGoesThroughOneFunction(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}

	fatalBody := regexp.MustCompile(`(?s)func \(a \*App\) fatal\([^\n]*\) \{(.*?)\n\}`).
		FindSubmatch(source)
	if fatalBody == nil {
		t.Fatal("App.fatal was not found in app.go")
	}

	// Signals first, then the drain: a drain that cannot be interrupted is the
	// one an operator meets.
	restore := bytes.Index(fatalBody[1], []byte("a.stop()"))
	drainAndExit := bytes.Index(fatalBody[1], []byte("utils.FatalAfter("))
	switch {
	case restore < 0:
		t.Error("App.fatal does not restore the signal disposition, so the drain it starts " +
			"swallows every later signal")
	case drainAndExit < 0:
		t.Error("App.fatal does not drain and exit")
	case restore > drainAndExit:
		t.Error("App.fatal drains before restoring signals, so the drain cannot be interrupted")
	}

	// And nothing may bypass it. Walked as syntax, so a comment does not count
	// as a call; matched by import path, so an aliased import counts; over every
	// declaration, so a package-level func literal counts; and over api and its
	// subpackages, since Initialize builds from both.
	allowed := map[string]string{
		utilsPath + ".FatalAfter": "fatal",
		// Runs first in Initialize, before anything exists to drain or restore.
		utilsPath + ".LogFatal": "initSentry",
	}
	found := 0
	for _, site := range exitSites(t) {
		if site.dir != "." || allowed[site.call] != site.function {
			t.Errorf("%s calls %s: every exit under api goes through App.fatal, or it skips "+
				"the signal restore and the drain", site.where, site.call)
			continue
		}
		found++
	}
	// A walk that finds nothing passes everything.
	if found != len(allowed) {
		t.Errorf("found %d of the %d allowed exits: the walk is not seeing the code", found, len(allowed))
	}
}

const utilsPath = "github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"

// exitFuncs is every function that ends the process, by import path.
var exitFuncs = map[string]bool{
	utilsPath + ".FatalAfter": true, utilsPath + ".LogFatal": true,
	"os.Exit": true, "syscall.Exit": true,
	"log.Fatal": true, "log.Fatalf": true, "log.Fatalln": true,
}

type exitSite struct {
	call, dir, function, where string
}

// exitSites lists every direct call to an exitFuncs entry in the non-test
// files under api, with its directory and top-level function ("" outside
// one). Not seen: a call through a function value (`die := utils.LogFatal`),
// or an exit inside another package that api calls.
func exitSites(t *testing.T) []exitSite {
	t.Helper()
	fset := token.NewFileSet()
	var sites []exitSite
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		imports, dotImports := importNames(file)
		for _, decl := range file.Decls {
			function := ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				function = fn.Name.Name
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, name := range calledNames(call, imports, dotImports) {
					if exitFuncs[name] {
						sites = append(sites, exitSite{name, filepath.Dir(path), function,
							fset.Position(call.Pos()).String()})
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sites
}

// importNames maps each name a file refers to an import by to its path, and
// lists the dot imports, whose functions are called by bare name.
func importNames(file *ast.File) (names map[string]string, dot []string) {
	names = map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "." {
			dot = append(dot, path)
			continue
		}
		names[name] = path
	}
	return names, dot
}

// calledNames is what a call can resolve to, as "importpath.Func": pkg.Func
// through the file's imports, or a bare Func through any of its dot imports.
func calledNames(call *ast.CallExpr, imports map[string]string, dotImports []string) []string {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok {
			return []string{imports[pkg.Name] + "." + fun.Sel.Name}
		}
	case *ast.Ident:
		names := make([]string, 0, len(dotImports))
		for _, path := range dotImports {
			names = append(names, path+"."+fun.Name)
		}
		return names
	}
	return nil
}

// A signal during the database connect is a stop, not a crash. Threading the
// startup context into initDB without this made a SIGTERM mid-connect exit 1 —
// past serverFn's own branch for it, because a.fatal exits rather than returning
// up through Initialize.
func TestACancelledConnectIsNotAFatal(t *testing.T) {
	if os.Getenv("CANCELLED_CONNECT_CHILD") == "1" {
		saved := *common.Config
		defer func() { *common.Config = saved }()
		// Nothing listening, so the connect would fail on its own merits too —
		// what is under test is which of the two reasons wins.
		common.Config.PgHost, common.Config.PgPort = "127.0.0.1", "1"

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		app := App{eventEmitter: &stubEmitter{}}
		app.initDB(ctx)
		fmt.Fprintln(os.Stderr, "RETURNED")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestACancelledConnectIsNotAFatal", "-test.timeout=60s")
	child.Env = append(os.Environ(), "CANCELLED_CONNECT_CHILD=1")
	out, err := child.CombinedOutput()
	output := string(out)

	if err != nil {
		t.Errorf("a cancelled connect exited non-zero (%v), so a clean stop is recorded as a "+
			"crashed startup:\n%s", err, output)
	}
	if !strings.Contains(output, "RETURNED") {
		t.Errorf("initDB did not return on a cancelled context:\n%s", output)
	}
}

// The same rule for the OIDC discovery fetch: a signal during it is a stop,
// and the same fetch failing with no signal is still a fatal.
func TestACancelledDiscoveryFetchIsNotAFatal(t *testing.T) {
	if mode := os.Getenv("DISCOVERY_CHILD"); mode != "" {
		// Nothing listening, so the fetch fails either way; what is under test
		// is which reason wins.
		common.Config.KeycloakServerUrl, common.Config.KeycloakRealm = "http://127.0.0.1:1", "x"
		ctx, cancel := context.WithCancel(context.Background())
		if mode == "cancelled" {
			cancel()
		}
		defer cancel()
		new(App).initGinEngine(ctx)
		fmt.Fprintln(os.Stderr, "RETURNED")
		return
	}

	run := func(mode string) (string, error) {
		child := exec.Command(os.Args[0], "-test.run=^TestACancelledDiscoveryFetchIsNotAFatal$")
		child.Env = append(os.Environ(), "DISCOVERY_CHILD="+mode)
		out, err := child.CombinedOutput()
		return string(out), err
	}

	if out, err := run("cancelled"); err != nil || !strings.Contains(out, "RETURNED") {
		t.Errorf("a signal during the discovery fetch exited (%v), so a clean stop is "+
			"recorded as a crashed startup:\n%s", err, out)
	}
	if out, err := run("live"); err == nil || strings.Contains(out, "RETURNED") {
		t.Errorf("an unreachable Keycloak with no signal did not exit 1 (%v):\n%s", err, out)
	}
}

// On the graceful path serve restores the signals before the request grace and
// the drain, which together can take most of a minute, so a second Ctrl-C can
// still cut them short. The request here is held open until stop runs: called
// first, it lets the grace end at once; called after, the grace runs its 15s.
func TestServeRestoresSignalsBeforeTheRequestGrace(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	stopped := make(chan struct{})
	var stopOnce sync.Once
	release := func() { stopOnce.Do(func() { close(stopped) }) }
	defer release()

	inHandler := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(inHandler)
		<-stopped
	})}
	app := &App{stop: release}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		app.serve(ctx, server, func() error { return server.Serve(listener) })
	}()
	go func() { _, _ = http.Get("http://" + listener.Addr().String()) }()

	select {
	case <-inHandler:
	case <-time.After(3 * time.Second):
		t.Fatal("the request never reached the handler")
	}
	cancel()

	select {
	case <-served:
	case <-time.After(3 * time.Second):
		t.Fatal("serve is still in the request grace with the signal still trapped: stop must " +
			"run before stopServer, or a second Ctrl-C does nothing during it")
	}
}

// A database that accepts the connection and never answers times out the
// connect. That is a failure and must exit 1, not be mistaken for a shutdown and
// leave the server running with no repo.
func TestATimedOutConnectIsAFatal(t *testing.T) {
	if os.Getenv("TIMED_OUT_CONNECT_CHILD") == "1" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, "LISTEN FAILED", err)
			return
		}
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer conn.Close() // held open, never answered
			}
		}()
		host, port, _ := net.SplitHostPort(ln.Addr().String())
		common.Config.PgHost, common.Config.PgPort = host, port
		dbConnectTimeout = 300 * time.Millisecond

		app := App{eventEmitter: &stubEmitter{}}
		app.initDB(context.Background())
		fmt.Fprintln(os.Stderr, "RETURNED")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestATimedOutConnectIsAFatal", "-test.timeout=60s")
	child.Env = append(os.Environ(), "TIMED_OUT_CONNECT_CHILD=1")
	out, err := child.CombinedOutput()
	output := string(out)

	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Errorf("a timed-out connect did not exit 1 (%v):\n%s", err, output)
	}
	if strings.Contains(output, "RETURNED") {
		t.Errorf("initDB returned on a timed-out connect, leaving the server with no repo:\n%s", output)
	}
}

// App.Shutdown's own ladder has to fit inside the backstop FatalAfter applies to
// it, or a drain that is bounded and progressing gets cut short by the generic
// bound.
func TestTheShutdownLadderFitsTheFatalBackstop(t *testing.T) {
	needed := shutdownBudget + sentryFlushGrace

	if needed >= utils.CleanupBackstop {
		t.Errorf("App.Shutdown needs %v (budget %v + flush %v) against FatalAfter's %v backstop, "+
			"so a fatal would cut its own drain short", needed, shutdownBudget, sentryFlushGrace,
			utils.CleanupBackstop)
	}
}
