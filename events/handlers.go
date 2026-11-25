package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
)

type EventHandler interface {
	Handle(context.Context, Event)
	Close(context.Context) error
}

type LoggerEventHandler struct{}

func (eh *LoggerEventHandler) Handle(ctx context.Context, event Event) {
	utils.LogFor(ctx).Debug("Handle event", slog.Group("event",
		slog.String("id", event.ID),
		slog.String("type", event.Type),
		slog.String("actor", utils.StrTruncate(event.Actor, 32)),
		slog.String("component", event.Component),
		slog.String("request_id", event.RequestID),
		slog.Any("payload", event.Payload),
	))
}

func (eh *LoggerEventHandler) Close(_ context.Context) error {
	return nil
}

type NatsEventHandler struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	ncClosed chan struct{}
}

// DrainTimeout bounds the subscription phase of nats.go's drain. CloseBudget is
// what a caller must allow for the whole drain: after the subscriptions,
// drainConnection runs an unconditional FlushTimeout(5s) before closing.
const (
	DrainTimeout     = 3 * time.Second
	natsFlushTimeout = 5 * time.Second

	CloseBudget = DrainTimeout + natsFlushTimeout + time.Second
)

func NewNatsEventHandler() (*NatsEventHandler, error) {
	eh := new(NatsEventHandler)
	// Buffered: once Close gives up on its deadline nothing receives, and
	// closedCallback would block forever on the send.
	eh.ncClosed = make(chan struct{}, 1)

	var err error
	eh.nc, err = nats.Connect(common.Config.NatsUrl,
		nats.ClosedHandler(eh.closedCallback),
		// drainConnection reports an abandoned drain only here, then closes anyway.
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, natsErr error) {
			subject := ""
			if sub != nil {
				subject = sub.Subject
			}
			slog.Error("nats async error", slog.String("subject", subject), slog.Any("err", natsErr))
			sentry.CaptureException(natsErr)
		}),
		// The library default is 30s, longer than any caller waits.
		nats.DrainTimeout(DrainTimeout))
	if err != nil {
		return nil, fmt.Errorf("nats.Connect: %w", err)
	}

	// Failures from here on must close the connection: the caller gets a nil
	// emitter and has nothing to close.
	eh.js, err = jetstream.New(eh.nc)
	if err != nil {
		eh.nc.Close()
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
		eh.nc.Close()
		return nil, fmt.Errorf("jetstream.CreateOrUpdateStream: %w", err)
	}

	return eh, nil
}

func (eh *NatsEventHandler) Handle(ctx context.Context, event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		utils.LogFor(ctx).Error("NatsEventHandler.Handle: json.Marshal", slog.String("event_id", event.ID), slog.Any("err", err))
		utils.SentryFor(ctx).CaptureException(err)
		return
	}

	subject := fmt.Sprintf("vh-srv-orders.%s", strings.ToLower(event.Type))

	_, err = eh.js.Publish(ctx, subject, payload, jetstream.WithRetryAttempts(3))
	if err != nil {
		utils.LogFor(ctx).Error("NatsEventHandler.Handle: jetstream.Publish", slog.String("event_id", event.ID), slog.Any("err", err))
		utils.SentryFor(ctx).CaptureException(err)
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
	slog.Info("nats connection closed")
	eh.ncClosed <- struct{}{}
}
