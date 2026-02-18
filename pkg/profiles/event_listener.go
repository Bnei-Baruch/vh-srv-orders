package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

const (
	ComponentAPI                          = "api"
	ComponentMembershipInvalidator        = "membership_invalidator"
	ComponentMembershipMigrator           = "membership_migrator"
	ComponentMembershipBulkEval           = "membership_bulk_eval"
	ComponentMembershipOrdersEventHandler = "membership_orders_event_handler"

	TypeCreateProfile     = "create_profile"
	TypeUpdateProfile     = "update_profile"
	TypeDeleteProfile     = "delete_profile"
	TypeHardDeleteProfile = "hard_delete_profile"
	TypeMergeAccounts     = "merge_accounts"
)

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Component string                 `json:"component"`
	Actor     string                 `json:"actor"`
	Payload   map[string]interface{} `json:"payload"`
}

type EventHandler func(Event)

type EventListener struct {
	nc          *nats.Conn
	js          jetstream.JetStream
	consumer    jetstream.Consumer
	consumerCtx jetstream.ConsumeContext

	queue    chan Event
	handlers []EventHandler
}

func NewEventListener() (*EventListener, error) {
	el := new(EventListener)

	var err error
	el.nc, err = nats.Connect(common.Config.NatsUrl)
	if err != nil {
		return nil, fmt.Errorf("nats.Connect: %w", err)
	}

	el.js, err = jetstream.New(el.nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	el.consumer, err = el.js.CreateOrUpdateConsumer(ctx, "VH_SRV_PROFILE", jetstream.ConsumerConfig{
		Name:        common.ServiceName,
		Durable:     common.ServiceName,
		Description: "Events listener of vh-srv-orders for profile changes",
	})
	if err != nil {
		return nil, fmt.Errorf("jetstream.CreateOrUpdateConsumer: %w", err)
	}

	el.queue = make(chan Event, 2^10)
	el.handlers = make([]EventHandler, 0)
	return el, nil
}

func (el *EventListener) Run() error {
	var err error
	el.consumerCtx, err = el.consumer.Consume(el.handleMessage)
	if err != nil {
		return fmt.Errorf("jetstream consumer.Consume: %w", err)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("EventListener.Run panic", slog.Any("err", err))
				sentry.CurrentHub().Recover(r)
				debug.PrintStack()

				el.consumerCtx.Stop()
				if err := el.Run(); err != nil {
					slog.Error("EventListener.Run error re-run after panic", slog.Any("err", err))
					sentry.CaptureException(err)
					panic(r)
				}
			}
		}()

		for event := range el.queue {
			for _, handler := range el.handlers {
				handler(event)
			}
		}
		slog.Debug("EventListener runner goroutine exit")
	}()

	return nil
}

func (el *EventListener) Close() {
	el.consumerCtx.Stop()
	el.nc.Close()
	close(el.queue)
}

func (el *EventListener) RegisterHandler(handler EventHandler) {
	el.handlers = append(el.handlers, handler)
}

func (el *EventListener) handleMessage(msg jetstream.Msg) {
	slog.Debug("EventListener.handleMessage", slog.Any("data", msg.Data()))

	var event Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("EventListener.handleMessage json.Unmarshal", slog.Any("err", err))
		sentry.CaptureException(err)
	}

	el.queue <- event

	msg.Ack()
}
