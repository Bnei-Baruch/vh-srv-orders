package eventstest

import (
	"context"
	"testing"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

func WithTestEventBuilder(t *testing.T, ctx context.Context) context.Context {
	return context.WithValue(ctx, common.CtxEventBuilder, NewTestEventBuilder(t))
}

type TestEventBuilder struct {
	t *testing.T
}

func NewTestEventBuilder(t *testing.T) *TestEventBuilder {
	return &TestEventBuilder{t: t}
}

func (tvb *TestEventBuilder) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	e := events.MakeEvent(eventType, payload)
	e.Actor = tvb.t.Name()
	e.Component = "test"
	return e
}
