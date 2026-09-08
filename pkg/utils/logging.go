package utils

import (
	"context"
	"log/slog"
	"os"

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
func FatalAfter(cleanup func(), msg string, args ...any) {
	slog.Error(msg, args...)
	if cleanup != nil {
		cleanup()
	}
	os.Exit(1)
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
