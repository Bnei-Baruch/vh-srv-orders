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

	queue    chan queued
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

// queued is a delivered message and its decoded event. The message travels with
// the event so the ack happens after the handlers have run.
type queued struct {
	event Event
	msg   jetstream.Msg
}

// queueCapacity is how many delivered events wait for the runner.
//
// It was written as `2^10`, which reads as 1024 and is not: Go has no power
// operator, so that expression is 2 XOR 10 = 8. Kept at 8 rather than
// "corrected" upwards: nothing here is acknowledged until a handler has run, so
// a deeper queue buys throughput this service has never needed while adding
// work a shutdown has to get through. A full queue blocks the delivery
// callback, which is backpressure, and unacked events are redelivered.
const queueCapacity = 8

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

	el.queue = make(chan queued, queueCapacity)
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

	for {
		select {
		case item := <-el.queue:
			el.deliver(item)
		case <-el.quit:
			el.drainQueue()
			slog.Debug("EventListener runner goroutine exit")
			return
		}
	}
}

// deliver runs the handlers for one event and acknowledges it.
//
// The panic is recovered per event. It used to be recovered for the whole
// runner, which then stopped the consumer and called Run again from this
// goroutine: that wrote consumerCtx and runnerStarted while Close was reading
// them, and — because deferred calls run last-in-first-out — closed done before
// the restarted runner existed. One handler panic therefore spent the handshake
// for the life of the process, and every later Close stopped waiting. A single
// bad event no longer costs the listener anything.
//
// The ack happens after the handlers have had their chance, panic or not. Not
// before: an event acknowledged without being handled is neither redelivered
// nor processed, which is how a shutdown could swallow one. Not conditional on
// success either: the handlers report failure by logging it, so a redelivery
// would repeat the same outcome and a poison event would loop for ever.
func (el *EventListener) deliver(item queued) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("EventListener handler panic",
				slog.Any("panic", r), slog.String("event_type", item.event.Type))
			sentry.CurrentHub().Recover(r)
			debug.PrintStack()
		}

		if item.msg != nil {
			item.msg.Ack()
		}
	}()

	for _, handler := range el.handlers {
		handler(item.event)
	}
}

// drainQueue delivers what is already buffered and returns. Anything it does not
// reach is unacknowledged and comes back on the next start.
func (el *EventListener) drainQueue() {
	for {
		select {
		case item := <-el.queue:
			el.deliver(item)
		default:
			return
		}
	}
}

// Close stops delivery, waits for the runner to finish, and only then drops the
// connection. Callers rely on that order: App.Shutdown closes the repo next, and
// these handlers write through it.
//
// The consumer is stopped before quit is signalled, so no callback can still be
// choosing between the queue and quit once the runner has gone. Signalling first
// left that race: both cases ready, Go picks either, and the send could land
// after the runner had stopped — acknowledging an event nothing would handle.
//
// It is called from the fatal paths too, including the one where Run itself
// failed, so nothing here is guaranteed to exist. The queue is deliberately not
// closed — the delivery callback may be blocked on a send to it, and closing it
// under that is a `send on closed channel` panic on the NATS dispatch goroutine,
// which would take the process down before the rest of the drain.
func (el *EventListener) Close() {
	if el.consumerCtx != nil {
		el.consumerCtx.Stop()
	}
	if el.quit != nil {
		el.quitOnce.Do(func() { close(el.quit) })
	}

	// Only if the runner is up: Consume failing means nothing will ever close
	// done, and waiting would hang the shutdown it was called to make orderly.
	if el.runnerStarted && el.done != nil {
		select {
		case <-el.done:
		case <-time.After(drainGrace):
			slog.Warn("EventListener.Close: runner did not stop in time, continuing",
				slog.Duration("grace", drainGrace))
		}
	}

	if el.nc != nil {
		el.nc.Close()
	}
}

// drainGrace bounds how long Close waits for the runner.
//
// The wait cannot be unbounded: the handlers reach the profile service over HTTP
// with no deadline of their own, so a black-holed connection would mean a
// shutdown that never finishes — the pool never closed, NATS never drained,
// and on a fatal path a process that never exits at all. Overshoot is reported
// and the drain carries on.
//
// Sized to fit inside a container's grace period alongside everything else that
// runs on the way out: the HTTP server's own 15s, then this, the emitter's 5s
// and sentry.Flush's 2s.
const drainGrace = 5 * time.Second

func (el *EventListener) RegisterHandler(handler EventHandler) {
	el.handlers = append(el.handlers, handler)
}

func (el *EventListener) handleMessage(msg jetstream.Msg) {
	slog.Debug("EventListener.handleMessage", slog.Any("data", msg.Data()))

	var event Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("EventListener.handleMessage json.Unmarshal", slog.Any("err", err))
		sentry.CaptureException(err)
		// Acked and dropped: the payload will never parse, so redelivering it
		// only repeats this. Without the return a zero-value Event went to the
		// handlers, hit the default branch of their type switch, and the
		// corruption vanished.
		msg.Ack()
		return
	}

	// Dropped events are left unacknowledged, so they come back on the next
	// start. The select is also what makes closing the queue unnecessary.
	select {
	case el.queue <- queued{event: event, msg: msg}:
	case <-el.quit:
		slog.Debug("EventListener.handleMessage dropped during shutdown")
	}
}
