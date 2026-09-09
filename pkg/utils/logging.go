package utils

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/getsentry/sentry-go"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func LogFatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// FatalAfter logs why the process is dying, runs cleanup, then exits. LogFatal
// is os.Exit, which runs no deferred function, so a `defer cleanup()` does not
// survive a fatal and the drain has to be called on the way out.
//
// The message goes first because the drain can be slow or can hang: closing an
// event emitter against a broken NATS spends its context and then reports the
// failure through a synchronous Sentry transport, and pgxpool.Close waits for
// every acquired connection to come back. Draining first buries the reason
// under that, or loses it entirely.
//
// cleanup may be nil, for the fatal paths that have nothing to drain yet.
//
// A panic in cleanup does not change the outcome. cleanup is arbitrary shutdown
// code, and if it unwinds instead of returning then os.Exit is never reached:
// the process dies with status 2 rather than 1, and any `defer cleanup()` it
// was called to substitute for runs a second time on the way out. Recovered and
// logged, so the exit is still the exit.
func FatalAfter(cleanup func(), msg string, args ...any) {
	slog.Error(msg, args...)
	if cleanup != nil {
		drain(cleanup)
	}
	os.Exit(1)
}

// cleanupBackstop bounds cleanup for the callers that do not bound themselves.
//
// api.App.Shutdown budgets its own steps; the others hand this a closure that
// reaches pgxpool.Close, which waits for every acquired connection to come back
// and takes no context. So a `billing start` that fails with its workers still
// holding connections used to exit 1 and now hung here instead — the exit this
// function exists to guarantee, lost to the drain it added.
//
// Sits above App.Shutdown's own budget on purpose, so a drain that is bounded
// and making progress is never cut short by this one.
// A var, not a const, so a test can shorten it and observe that FatalAfter
// actually applies it. As a parameter at the call site it was possible to pass
// something else — or nothing — with the constant still sitting here looking
// right.
var cleanupBackstop = 30 * time.Second

// drain runs cleanup, recovered and bounded. Returning matters more than
// finishing: the caller's next statement is the exit.
func drain(cleanup func()) {
	budget := cleanupBackstop

	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic while draining on the fatal path", slog.Any("panic", r))
			}
		}()

		cleanup()
	}()

	select {
	case <-done:
	case <-time.After(budget):
		slog.Error("draining on the fatal path did not finish, exiting anyway",
			slog.Duration("budget", budget))
	}
}

func LogFor(ctx context.Context) *slog.Logger {
	if val := ctx.Value(common.CtxLogger); val != nil {
		if logger, ok := val.(*slog.Logger); ok {
			return logger
		}
	}
	return slog.Default()
}

func SentryFor(ctx context.Context) *sentry.Hub {
	if val := sentry.GetHubFromContext(ctx); val != nil {
		return val
	}
	return sentry.CurrentHub()
}
