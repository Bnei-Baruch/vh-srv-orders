package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
)

// fakeServer records the order of everything runServer does.
type fakeServer struct {
	initErr error
	calls   *[]string
}

func (f fakeServer) Initialize(context.Context, func()) error {
	*f.calls = append(*f.calls, "initialize")
	return f.initErr
}
func (f fakeServer) Run(context.Context) { *f.calls = append(*f.calls, "run") }
func (f fakeServer) Shutdown()           { *f.calls = append(*f.calls, "shutdown") }

func runFake(t *testing.T, initErr error) []string {
	t.Helper()
	var calls []string
	stop := func() { calls = append(calls, "stop") }
	fatal := func(func(), string, ...any) { calls = append(calls, "fatal") }
	runServer(context.Background(), stop, fakeServer{initErr: initErr, calls: &calls}, fatal)
	return calls
}

// A signal during Initialize is a clean stop: no fatal (exit 1), and the drain
// still runs, which only the defer placed before Initialize provides.
func TestRunServer_ASignalDuringStartupDrainsAndIsNotFatal(t *testing.T) {
	calls := runFake(t, fmt.Errorf("initEventListener: %w", context.Canceled))

	want := []string{"initialize", "stop", "shutdown"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls %v, want %v: a cancelled startup must restore signals, drain, and "+
			"not exit 1", calls, want)
	}
}

func TestRunServer_AFailedStartupIsFatalAfterRestoringSignals(t *testing.T) {
	calls := runFake(t, errors.New("db down"))

	if !slices.Equal(calls[:3], []string{"initialize", "stop", "fatal"}) {
		t.Errorf("calls %v: a failed startup must restore signals, then go fatal", calls)
	}
	if !slices.Contains(calls, "shutdown") {
		t.Errorf("calls %v: nothing drained", calls)
	}
}

func TestRunServer_ANormalRunDrainsAfterRestoringSignals(t *testing.T) {
	calls := runFake(t, nil)

	want := []string{"initialize", "run", "stop", "shutdown"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls %v, want %v", calls, want)
	}
}
