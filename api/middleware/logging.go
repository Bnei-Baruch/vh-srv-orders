package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nuid"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		params := map[string]string{}
		for _, p := range c.Params {
			params[p.Key] = p.Value
		}

		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = nuid.Next()
		}
		c.Header("X-Request-ID", rid)

		logger := slog.Default().With(slog.String("request_id", rid))
		ctx := context.WithValue(c.Request.Context(), common.CtxRequestID, rid)
		ctx = context.WithValue(ctx, common.CtxLogger, logger)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		status := c.Writer.Status()

		attributes := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("route", c.FullPath()),
			slog.String("path", path),
			slog.String("query", query),
			slog.Int("status", status),
			slog.Duration("latency", time.Now().Sub(start)),
			slog.String("ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
		}

		if len(params) > 0 {
			paramsAttrs := []slog.Attr{}
			for k, v := range params {
				paramsAttrs = append(paramsAttrs, slog.String(k, v))
			}
			attributes = append(attributes, slog.Attr{Key: "params", Value: slog.GroupValue(paramsAttrs...)})
		}

		level := slog.LevelInfo
		if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
			level = slog.LevelWarn
		} else if status >= http.StatusInternalServerError {
			level = slog.LevelError
			attributes = append(attributes, slog.Any("errs", c.Errors))
		}

		logger.LogAttrs(c.Request.Context(), level, "request", attributes...)
	}
}
