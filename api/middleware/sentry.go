package middleware

import (
	"net/http"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func Sentry() gin.HandlerFunc {
	return func(c *gin.Context) {
		hub := sentrygin.GetHubFromContext(c)

		// enrich scope and set on request context
		if hub != nil {
			rCtx := c.Request.Context()
			hub.Scope().SetTag("request_id", rCtx.Value(common.CtxRequestID).(string))
			c.Request = c.Request.WithContext(sentry.SetHubOnContext(rCtx, hub))
		}

		c.Next()

		status := c.Writer.Status()

		// skip < 5xx
		if status < http.StatusInternalServerError {
			return
		}

		if hub != nil {
			for _, err := range c.Errors {
				hub.CaptureException(err)
			}
		}
	}
}
