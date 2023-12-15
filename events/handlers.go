package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

type EventHandler interface {
	Handle(Event)
	Close(context.Context) error
}

type LoggerEventHandler struct{}

func (eh *LoggerEventHandler) Handle(event Event) {
	log.Printf("INFO: event ID %s, Type %s, Payload %v\n", event.ID, event.Type, event.Payload)
}

func (eh *LoggerEventHandler) Close(_ context.Context) error {
	return nil
}

type NatsEventHandler struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	ncClosed chan struct{}
}

func NewNatsEventHandler() (*NatsEventHandler, error) {
	eh := new(NatsEventHandler)
	eh.ncClosed = make(chan struct{})

	var err error
	eh.nc, err = nats.Connect(common.Config.NatsUrl, nats.ClosedHandler(eh.closedCallback))
	if err != nil {
		return nil, fmt.Errorf("nats.Connect: %w", err)
	}

	eh.js, err = jetstream.New(eh.nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = eh.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        "VH_SRV_ORDERS",
		Description: "Events stream of vh-srv-orders",
		Subjects:    []string{"vh-srv-orders.*"},
		MaxMsgs:     65536, // 2^16
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("jetstream.CreateOrUpdateStream: %w", err)
	}

	return eh, nil
}

func (eh *NatsEventHandler) Handle(event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("ERROR: NatsEventHandler.Handle: json.Marshal event [%s]: %s\n", event.ID, err.Error())
		return
	}

	subject := fmt.Sprintf("vh-srv-orders.%s", strings.ToLower(event.Type))

	_, err = eh.js.Publish(context.Background(), subject, payload, jetstream.WithRetryAttempts(3))
	if err != nil {
		log.Printf("ERROR: NatsEventHandler.Handle: jetstream.Publish: %v\n", err)
	}
}

func (eh *NatsEventHandler) Close(ctx context.Context) error {
	if err := eh.nc.Drain(); err != nil {
		return fmt.Errorf("NatsEventHandler: nc.Drain(): %w", err)
	}

	select {
	case <-eh.ncClosed:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("NatsEventHandler drain ctx.Done: %w", ctx.Err())
	}
}

func (eh *NatsEventHandler) closedCallback(conn *nats.Conn) {
	log.Println("nats connection closed")
	eh.ncClosed <- struct{}{}
}
