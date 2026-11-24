package utils

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

func LogFatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// FatalAfter logs why the process is dying, runs cleanup, then exits 1.
// LogFatal is os.Exit, which skips deferred functions, so a `defer cleanup()`
// never drains on a fatal path. The message goes first because the drain can be
// slow or hang. cleanup may be nil; a panic in it is recovered so the exit code
// stays 1.
func FatalAfter(cleanup func(), msg string, args ...any) {
	slog.Error(msg, args...)
	if cleanup != nil {
		drain(cleanup)
	}
	os.Exit(1)
}

// CleanupBackstop bounds cleanup: most callers' cleanup reaches pgxpool.Close,
// which waits for every acquired connection and takes no context. It sits above
// App.Shutdown's own budget so a bounded drain is never cut short. A var so a
// test can shorten it.
var CleanupBackstop = 30 * time.Second

// drain runs cleanup, recovered and bounded: the caller's next statement is the exit.
func drain(cleanup func()) {
	budget := CleanupBackstop
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
