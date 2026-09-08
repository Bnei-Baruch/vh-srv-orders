package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// fatalAfter ends in os.Exit, so it is exercised in a child process — the only
// way to see both that the drain ran and that the exit still happened.
//
// The ordering is the point: `defer cleanup()` before a utils.LogFatal reads as
// correct and drains nothing, which is the defect this replaces.
func TestFatalAfterDrainsBeforeExiting(t *testing.T) {
	if os.Getenv("FATAL_AFTER_CHILD") == "1" {
		fatalAfter(func() { fmt.Fprintln(os.Stderr, "DRAINED") }, "child exiting")
		fmt.Fprintln(os.Stderr, "REACHED UNREACHABLE")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestFatalAfterDrainsBeforeExiting")
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
		t.Fatalf("the fatal message is missing:\n%s", output)
	case drained > fatal:
		t.Fatalf("cleanup ran after the fatal message:\n%s", output)
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
