package events

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

type EventEmitter interface {
	Emit(...Event)
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

func (e *NoopEmitter) Emit(_ ...Event)         {}
func (e *NoopEmitter) Close(_ context.Context) {}

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

func (e *SimpleEmitter) Emit(events ...Event) {
	for _, event := range events {
		event.ID = ulid.MustNew(ulid.Now(), e.entropy).String()
		for _, handler := range e.handlers {
			handler.Handle(event)
		}
	}
}

func (e *SimpleEmitter) Close(ctx context.Context) {
	log.Println("Closing event emitter")
	for _, handler := range e.handlers {
		if err := handler.Close(ctx); err != nil {
			log.Printf("ERROR: close event handler: %v\n", err)
		}
	}
}
