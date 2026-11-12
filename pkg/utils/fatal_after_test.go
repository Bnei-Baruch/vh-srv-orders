package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fatalAfter ends in os.Exit, so it is exercised in a child process — the only
// way to see the message, the drain and the exit in one run.
//
// The order is the assertion: the reason has to be logged before the drain,
// which can spend a 5s context on a broken NATS or block in pgxpool.Close.
// Draining first buries the reason or loses it.
func TestFatalAfterLogsBeforeDrainingAndExits(t *testing.T) {
	if os.Getenv("FATAL_AFTER_CHILD") == "1" {
		FatalAfter(func() { fmt.Fprintln(os.Stderr, "DRAINED") }, "child exiting")
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
		FatalAfter(nil, "child exiting")
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

// A cleanup that panics must not change the outcome: still exit 1, not an
// unwind to 2, and not a second run of the deferred cleanup it stands in for.
func TestFatalAfterSurvivesAPanickingCleanup(t *testing.T) {
	if os.Getenv("FATAL_AFTER_PANIC_CHILD") == "1" {
		defer fmt.Fprintln(os.Stderr, "DEFERRED RAN")
		FatalAfter(func() { panic("drain exploded") }, "child exiting")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestFatalAfterSurvivesAPanickingCleanup")
	child.Env = append(os.Environ(), "FATAL_AFTER_PANIC_CHILD=1")
	out, err := child.CombinedOutput()
	output := string(out)

	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected a non-zero exit, got %v:\n%s", err, output)
	}
	if exit.ExitCode() != 1 {
		t.Errorf("exit code %d, want 1 — the panic changed the outcome:\n%s", exit.ExitCode(), output)
	}
	if !strings.Contains(output, "child exiting") {
		t.Errorf("the reason is missing:\n%s", output)
	}
	if !strings.Contains(output, "panic while draining") {
		t.Errorf("the drain panic was not reported:\n%s", output)
	}
	if strings.Contains(output, "DEFERRED RAN") {
		t.Errorf("the panic unwound instead of exiting, so deferred cleanup ran again:\n%s", output)
	}
}

// A cleanup that never returns must not become a process that never exits. Most
// callers hand FatalAfter a closure that reaches pgxpool.Close, which waits for
// every acquired connection and takes no context — so a failure with workers
// still holding connections hung here instead of exiting 1.
//
// drain is tested directly rather than through FatalAfter: the budget that
// matters in production is 30s, and what needs proving is that the wait is
// bounded at all.
func TestDrainGivesUpOnACleanupThatNeverReturns(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	restore := CleanupBackstop
	CleanupBackstop = 100 * time.Millisecond
	defer func() { CleanupBackstop = restore }()

	start := time.Now()
	drain(func() { <-release })
	waited := time.Since(start)

	if waited > 3*time.Second {
		t.Fatalf("drain waited %v on a cleanup that never returns", waited)
	}
	if waited < 100*time.Millisecond {
		t.Fatalf("drain returned after %v, before its own budget — nothing is being waited on",
			waited)
	}
}

// And FatalAfter has to be the one applying that bound. Testing drain alone left
// the wiring unpinned: handing it any other budget — or none — kept the package
// green with the backstop still sitting in the file looking right.
func TestFatalAfterAppliesTheCleanupBackstop(t *testing.T) {
	if os.Getenv("FATAL_AFTER_BACKSTOP_CHILD") == "1" {
		CleanupBackstop = 200 * time.Millisecond
		FatalAfter(func() { time.Sleep(time.Hour) }, "child exiting")
		fmt.Fprintln(os.Stderr, "RETURNED INSTEAD OF EXITING")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestFatalAfterAppliesTheCleanupBackstop",
		"-test.timeout=30s")
	child.Env = append(os.Environ(), "FATAL_AFTER_BACKSTOP_CHILD=1")

	start := time.Now()
	out, err := child.CombinedOutput()
	waited := time.Since(start)
	output := string(out)

	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("expected exit 1, got %v:\n%s", err, output)
	}
	if waited > 20*time.Second {
		t.Errorf("the child took %v: FatalAfter is not applying the backstop it is given", waited)
	}
	if !strings.Contains(output, "did not finish") {
		t.Errorf("the overshoot was not reported:\n%s", output)
	}
}
