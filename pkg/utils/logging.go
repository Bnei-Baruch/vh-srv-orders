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
