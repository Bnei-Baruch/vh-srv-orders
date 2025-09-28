package events

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

type EventEmitter interface {
	Emit(ctx context.Context, events ...Event)
	Close(ctx context.Context)
}

func CreateEmitter() (EventEmitter, error) {
	handlers := []EventHandler{new(LoggerEventHandler)}

	if common.Config.NatsUrl != "" {
		natsHandler, err := NewNatsEventHandler()
		if err != nil {
			return nil, fmt.Errorf("initialize nats handler: %w", err)
		}
		handlers = append(handlers, natsHandler)
	}

	return NewSimpleEmitter(handlers...), nil
}

type NoopEmitter struct{}

func (e *NoopEmitter) Emit(_ context.Context, _ ...Event) {}
func (e *NoopEmitter) Close(_ context.Context)            {}

type SimpleEmitter struct {
	entropy  io.Reader
	handlers []EventHandler
}

func NewSimpleEmitter(handlers ...EventHandler) *SimpleEmitter {
	e := new(SimpleEmitter)
	e.entropy = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	e.handlers = handlers
	return e
}

func (e *SimpleEmitter) Emit(ctx context.Context, events ...Event) {
	for _, event := range events {
		event.ID = ulid.MustNew(ulid.Now(), e.entropy).String()
		for _, handler := range e.handlers {
			handler.Handle(ctx, event)
		}
	}
}

func (e *SimpleEmitter) Close(ctx context.Context) {
	slog.Info("Closing event emitter")
	for _, handler := range e.handlers {
		if err := handler.Close(ctx); err != nil {
			slog.Error("close event handler", slog.Any("err", err))
			utils.SentryFor(ctx).CaptureException(err)
		}
	}
}
