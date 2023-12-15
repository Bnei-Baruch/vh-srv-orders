package middleware

import (
	"context"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

func EventsBuilder() gin.HandlerFunc {
	return func(c *gin.Context) {
		builder := NewApiEventBuilder(c.Request.UserAgent())
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), common.CtxEventBuilder, builder))

		c.Next()
	}
}

type ApiEventBuilder struct {
	actor string
}

func NewApiEventBuilder(actor string) *ApiEventBuilder {
	return &ApiEventBuilder{actor: actor}
}

func (a *ApiEventBuilder) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentAPI
	event.Actor = a.actor
	return event
}
