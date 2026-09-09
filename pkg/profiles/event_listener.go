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
	// callbacks counts delivery callbacks in flight, so Close can wait for the
	// ones Drain pushes through before dropping the connection they answer on.
	callbacks sync.WaitGroup
	// runnerStarted records that runQueue is up, so Close knows whether
	// anything will ever close done.
	runnerStarted bool
}

// ackWait is how long the server waits for an ack before redelivering.
//
// Derived, because it does not bound one handler — it bounds a queue. The ack
// happens after the handlers run, so a message prefetched into the last slot
// waits for every message ahead of it: queueCapacity handlers, each of which
// can spend requestTimeout on the profile service and again on a retry after a
// 401. Two minutes covered one call and none of the queueing, so a slow profile
// service redelivered events that were still sitting in the queue, and each
// redelivery published its own derived account event.
const ackWait = queueCapacity*2*requestTimeout + time.Minute

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

	el.consumer, err = el.js.CreateOrUpdateConsumer(ctx, "VH_SRV_PROFILE", consumerConfig())
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

// consumerConfig is separate so what it sets can be checked without a server.
func consumerConfig() jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Name:        common.ServiceName,
		Durable:     common.ServiceName,
		Description: "Events listener of vh-srv-orders for profile changes",
		// AckWait exists because the ack happens after the handlers run: the
		// server's 30s default would expire on anything the single-threaded
		// runner had not reached, and the redelivery would queue behind the same
		// backlog.
		//
		// MaxDeliver is deliberately left unset. Capping it without a dead
		// letter does not contain a poison event, it discards one: nothing here
		// consumes the MAX_DELIVERIES advisory, so the fifth delivery would be
		// the last and a create_profile for a real member would vanish with
		// nothing raised. Redelivery every AckWait is visible in the consumer's
		// pending count; silent loss is not.
		AckWait: ackWait,
	}
}

func (el *EventListener) Run() error {
	var err error
	// Prefetch bounded to what the queue can hold. Consume's default is 500
	// messages, all of which start their AckWait clock on delivery while the
	// runner works through them eight at a time.
	el.consumerCtx, err = el.consumer.Consume(el.handleMessage,
		jetstream.PullMaxMessages(queueCapacity))
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
// on the way out, so Close can wait for it — unless Close's grace expires
// first, in which case a handler does run on into a closed repo.
func (el *EventListener) runQueue() {
	defer el.doneOnce.Do(func() { close(el.done) })

	for {
		select {
		case item := <-el.queue:
			el.recovered("deliver", func() { el.deliver(item) })
		case <-el.quit:
			el.recovered("drainQueue", el.drainQueue)
			slog.Debug("EventListener runner goroutine exit")
			return
		}
	}
}

// deliver runs the handlers for one event and acknowledges it.
//
// A panic is recovered per handler, in callHandler. It used to be recovered for
// the whole runner, which then stopped the consumer and called Run again from this
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
		if item.msg == nil {
			return
		}
		// Logged, not ignored: past Close's grace this runs against a dropped
		// connection, and the event is then redelivered and handled twice.
		if err := item.msg.Ack(); err != nil {
			slog.Error("EventListener ack", slog.Any("err", err),
				slog.String("event_type", item.event.Type))
		}
	}()

	for _, handler := range el.handlers {
		el.callHandler(handler, item.event)
	}
}

// callHandler runs one handler, recovered.
//
// Per handler rather than per event: a panic in the first used to skip the rest,
// which is invisible today because exactly one is registered, and would be a
// silent gap the moment a second one is.
func (el *EventListener) callHandler(handler EventHandler, event Event) {
	el.recovered("handler:"+event.Type, func() { handler(event) })
}

// recovered runs fn, reporting a panic instead of letting it out.
//
// Every step the runner takes needs this, not only the handlers: a panic in
// deliver's deferred ack, or in drainQueue, would otherwise leave the goroutine
// and kill the process — skipping the drain this whole branch exists to
// perform. The self-restart removed in an earlier commit was catching those by
// accident, and took the net with it.
func (el *EventListener) recovered(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("EventListener panic", slog.String("in", what), slog.Any("panic", r))
			sentry.CurrentHub().Recover(r)
			debug.PrintStack()
		}
	}()

	fn()
}

// drainQueue delivers what is already buffered and returns. Anything it does not
// reach is unacknowledged and comes back on the next start.
func (el *EventListener) drainQueue() {
	for {
		select {
		case item := <-el.queue:
			// Per item, like the runner's own loop: recovering around the whole
			// drain would abandon everything behind a panicking event.
			el.recovered("deliver", func() { el.deliver(item) })
		default:
			return
		}
	}
}

// Close stops delivery and waits for the runner, so App.Shutdown can close the
// repo the handlers write through — but only up to DrainGrace. Past that it
// returns with the runner still going, and those handlers do meet a closed pool.
//
// The consumer is stopped before quit is signalled, which is the useful order
// but not a guarantee: ConsumeContext.Stop is asynchronous, so a callback can
// still be in handleMessage's select afterwards. Its send then stays buffered
// and unacked, which is why that select must keep its quit case.
//
// Called from the fatal paths too, including the one where Run itself failed, so
// nothing here is guaranteed to exist. The queue is never closed: the callback
// may be blocked sending to it, and closing it under that is a panic on the NATS
// dispatch goroutine.
func (el *EventListener) Close() {
	// Quit first, so anything Drain pushes through the callback below is handed
	// back rather than queued for a runner that is about to stop.
	if el.quit != nil {
		el.quitOnce.Do(func() { close(el.quit) })
	}

	// Drain, not Stop. Stop discards whatever the consumer has already
	// prefetched — up to queueCapacity messages whose AckWait clock is already
	// running and which the callback therefore never sees, so they are neither
	// handled nor handed back and come back only when the wait expires. Drain
	// pushes them through the callback, where the closed quit turns each into a
	// Nak and an immediate redelivery. It also means the quit branch in
	// handleMessage does real work at shutdown rather than catching the one
	// message that happened to be mid-callback.
	if el.consumerCtx != nil {
		el.consumerCtx.Drain()
	}

	// Those Naks travel on the connection dropped at the end of this function,
	// so the callbacks have to finish first.
	el.waitFor("callbacks", el.callbacksDone())

	// Only if the runner is up: Consume failing means nothing will ever close
	// done, and waiting would hang the shutdown it was called to make orderly.
	if el.runnerStarted && el.done != nil {
		el.waitFor("runner", el.done)
	}

	if el.nc != nil {
		el.nc.Close()
	}
}

// waitFor waits on ch for at most DrainGrace, reporting an overshoot rather than
// hanging the shutdown it is part of.
func (el *EventListener) waitFor(what string, ch <-chan struct{}) {
	select {
	case <-ch:
	case <-time.After(DrainGrace):
		slog.Warn("EventListener.Close gave up waiting, continuing",
			slog.String("on", what), slog.Duration("grace", DrainGrace))
	}
}

// callbacksDone closes once no delivery callback is in flight.
func (el *EventListener) callbacksDone() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		el.callbacks.Wait()
	}()
	return done
}

// DrainGrace bounds how long Close waits for the runner. Exported because the
// caller has to budget for it: api.App.Shutdown wraps this wait and its own.
//
// It cannot be unbounded — the handlers reach the profile service with no
// deadline of their own, so one black-holed connection would mean a shutdown
// that never finishes. Overshoot is reported and the drain carries on, which
// means a handler can outlive Close; see the note on Close.
const DrainGrace = 4 * time.Second

func (el *EventListener) RegisterHandler(handler EventHandler) {
	el.handlers = append(el.handlers, handler)
}

func (el *EventListener) handleMessage(msg jetstream.Msg) {
	el.callbacks.Add(1)
	defer el.callbacks.Done()

	slog.Debug("EventListener.handleMessage", slog.Any("data", msg.Data()))

	var event Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("EventListener.handleMessage json.Unmarshal", slog.Any("err", err))
		sentry.CaptureException(err)
		// Terminated rather than acked: Term says "never redeliver this",
		// which is what a payload that will never parse deserves, and it does
		// not depend on a delivery cap — MaxDeliver is unset, so a failed ack
		// here would come back every AckWait for ever, reporting to Sentry each
		// time. Without the return a zero-value Event went to the handlers, hit
		// the default branch of their type switch, and the corruption vanished.
		if termErr := msg.TermWithReason("unparseable payload"); termErr != nil {
			slog.Error("EventListener.handleMessage term", slog.Any("err", termErr))
		}
		return
	}

	// Quit checked on its own first. Both cases are otherwise ready at once
	// while there is queue space, and select picks at random — so a shutdown
	// already under way could still take on another event, which is the window
	// that lets a send land after the runner has stopped.
	select {
	case <-el.quit:
		el.drop(msg)
		return
	default:
	}

	select {
	case el.queue <- queued{event: event, msg: msg}:
	case <-el.quit:
		el.drop(msg)
	}
}

// drop hands an event back rather than holding it through a shutdown.
//
// Naked, not left silent: unacknowledged it would return anyway, but only after
// ackWait, which is sized for a full queue of slow handlers — nine minutes as
// configured. Nothing is working on a dropped event, so it should come back on
// the next start rather than nine minutes into it.
func (el *EventListener) drop(msg jetstream.Msg) {
	slog.Debug("EventListener dropped an event during shutdown")

	if err := msg.Nak(); err != nil {
		slog.Error("EventListener nak", slog.Any("err", err))
	}
}
