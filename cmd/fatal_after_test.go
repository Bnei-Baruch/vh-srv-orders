package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// fatalAfter ends in os.Exit, so it is exercised in a child process — the only
// way to see the message, the drain and the exit in one run.
//
// The order is the assertion: the reason has to be logged before the drain,
// which can spend a 5s context on a broken NATS or block in pgxpool.Close.
// Draining first buries the reason or loses it.
func TestFatalAfterLogsBeforeDrainingAndExits(t *testing.T) {
	if os.Getenv("FATAL_AFTER_CHILD") == "1" {
		fatalAfter(func() { fmt.Fprintln(os.Stderr, "DRAINED") }, "child exiting")
		fmt.Fprintln(os.Stderr, "REACHED UNREACHABLE")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestFatalAfterLogsBeforeDrainingAndExits")
	child.Env = append(os.Environ(), "FATAL_AFTER_CHILD=1")
	out, err := child.CombinedOutput()
	output := string(out)

	if err == nil {
		t.Fatalf("the child must exit non-zero:\n%s", output)
	}
	drained := strings.Index(output, "DRAINED")
	fatal := strings.Index(output, "child exiting")
	switch {
	case drained < 0:
		t.Fatalf("cleanup did not run:\n%s", output)
	case fatal < 0:
		t.Fatalf("the reason is missing:\n%s", output)
	case fatal > drained:
		t.Fatalf("the reason was logged after the drain, so a slow or hung drain "+
			"would bury or lose it:\n%s", output)
	}
	if strings.Contains(output, "REACHED UNREACHABLE") {
		t.Fatalf("fatalAfter returned instead of exiting:\n%s", output)
	}
}

// A nil cleanup is allowed, for the fatal paths that have nothing to drain yet.
func TestFatalAfterAcceptsNoCleanup(t *testing.T) {
	if os.Getenv("FATAL_AFTER_NIL_CHILD") == "1" {
		fatalAfter(nil, "child exiting")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestFatalAfterAcceptsNoCleanup")
	child.Env = append(os.Environ(), "FATAL_AFTER_NIL_CHILD=1")
	out, err := child.CombinedOutput()

	if err == nil {
		t.Fatalf("the child must exit non-zero:\n%s", out)
	}
	if !strings.Contains(string(out), "child exiting") {
		t.Fatalf("the fatal message is missing:\n%s", out)
	}
}

// buildChargeableBillingService runs after its callers' `defer cleanup()`, so a
// fatal inside it skips the drain — the pool stays open and NATS is dropped
// without draining. Its configuration checks were hoisted into the commands for
// that reason, and this stops them coming back.
func TestTheChargeBuilderDoesNotFatal(t *testing.T) {
	source, err := os.ReadFile("billing.go")
	if err != nil {
		t.Fatal(err)
	}

	body := regexp.MustCompile(`(?s)func buildChargeableBillingService\([^)]*\)[^{]*\{(.*?)\n\}`).
		FindSubmatch(source)
	if body == nil {
		t.Fatal("buildChargeableBillingService not found in billing.go — has it been renamed?")
	}

	if bytes.Contains(body[1], []byte("LogFatal")) {
		t.Error("buildChargeableBillingService must not exit: it is called after `defer cleanup()`, " +
			"so a fatal here skips the drain. Validate in the command, before initBillingInfra, " +
			"or return an error")
	}
}
