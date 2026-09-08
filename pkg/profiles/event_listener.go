package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
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

	// quit tells the delivery callback and the runner to stop; done is closed
	// by the runner once it has, so Close can wait for it. The queue is never
	// closed: closing it would race the callback's send.
	quit     chan struct{}
	done     chan struct{}
	quitOnce sync.Once
	doneOnce sync.Once
	// runnerStarted records that runQueue is up, so Close knows whether
	// anything will ever close done.
	runnerStarted bool
}

func NewEventListener() (*EventListener, error) {
	el := new(EventListener)

	var err error
	el.nc, err = nats.Connect(common.Config.NatsUrl)
	if err != nil {
		return nil, fmt.Errorf("nats.Connect: %w", err)
	}

	// Every failure past this point closes the connection: returning (nil, err)
	// leaves it open with no reference to it, and the caller has nothing to
	// close.
	el.js, err = jetstream.New(el.nc)
	if err != nil {
		el.nc.Close()
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
		el.nc.Close()
		return nil, fmt.Errorf("jetstream.CreateOrUpdateConsumer: %w", err)
	}

	el.queue = make(chan Event, 2^10)
	el.handlers = make([]EventHandler, 0)
	el.quit = make(chan struct{})
	el.done = make(chan struct{})
	return el, nil
}

func (el *EventListener) Run() error {
	var err error
	el.consumerCtx, err = el.consumer.Consume(el.handleMessage)
	if err != nil {
		return fmt.Errorf("jetstream consumer.Consume: %w", err)
	}

	el.runnerStarted = true
	go el.runQueue()

	return nil
}

// runQueue delivers queued events to the handlers until Close says stop.
//
// It selects on quit rather than ranging over a closed queue, and signals done
// on the way out — so Close can wait, and no handler runs after Close returns.
// That is what keeps the repo alive underneath these handlers: they write
// through it, and App.Shutdown closes it as soon as this has stopped.
func (el *EventListener) runQueue() {
	defer el.doneOnce.Do(func() { close(el.done) })
	defer func() {
		if r := recover(); r != nil {
			slog.Error("EventListener.runQueue panic", slog.Any("panic", r))
			sentry.CurrentHub().Recover(r)
			debug.PrintStack()

			if el.consumerCtx != nil {
				el.consumerCtx.Stop()
			}
			if err := el.Run(); err != nil {
				slog.Error("EventListener.Run error re-run after panic", slog.Any("err", err))
				sentry.CaptureException(err)
				panic(r)
			}
		}
	}()

	for {
		select {
		case event := <-el.queue:
			for _, handler := range el.handlers {
				handler(event)
			}
		case <-el.quit:
			// Buffered events are already acked — handleMessage acks on
			// enqueue — so dropping them loses them for good. Handled before
			// stopping, which is safe precisely because Close waits: the repo
			// they write through is still open.
			el.drainQueue()
			slog.Debug("EventListener runner goroutine exit")
			return
		}
	}
}

// drainQueue handles what is already buffered and returns. Bounded by the
// queue's capacity, so it cannot hold up a shutdown indefinitely.
func (el *EventListener) drainQueue() {
	for {
		select {
		case event := <-el.queue:
			for _, handler := range el.handlers {
				handler(event)
			}
		default:
			return
		}
	}
}

// Close stops delivery, waits for the runner to finish, and only then drops the
// connection. Callers rely on that order: App.Shutdown closes the repo next, and
// these handlers write through it.
//
// It is called from the fatal paths too, including the one where Run itself
// failed, so nothing here is guaranteed to exist. The queue is deliberately not
// closed — the delivery callback may be blocked on a send to it, and closing it
// under that is a `send on closed channel` panic on the NATS dispatch
// goroutine, which would take the process down before the rest of the drain.
func (el *EventListener) Close() {
	if el.quit != nil {
		el.quitOnce.Do(func() { close(el.quit) })
	}
	if el.consumerCtx != nil {
		el.consumerCtx.Stop()
	}

	// Only if the runner is up: Consume failing means nothing will ever close
	// done, and waiting would hang the shutdown it was called to make orderly.
	if el.runnerStarted && el.done != nil {
		<-el.done
	}

	if el.nc != nil {
		el.nc.Close()
	}
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

	// Not acked when shutting down: an event dropped here has not been handled,
	// so leaving it unacked is what gets it redelivered. The select is also what
	// makes closing the queue unnecessary — this send can never outlive Close.
	select {
	case el.queue <- event:
		msg.Ack()
	case <-el.quit:
		slog.Debug("EventListener.handleMessage dropped during shutdown")
	}
}
