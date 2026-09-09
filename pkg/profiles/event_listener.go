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

// natsConn is the part of *nats.Conn that Close uses. An interface so Close's
// own behaviour is testable: what it does with the connection is the whole
// subject of this file's shutdown path, and a source check could only assert
// which method name appears in it.
type natsConn interface {
	Drain() error
	Close()
}

type EventListener struct {
	nc          natsConn
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
	// ncClosed closes once the connection has finished draining, which is the
	// only signal that the delivery callbacks are done: both Drain calls in
	// Close are asynchronous.
	ncClosed     chan struct{}
	ncClosedOnce sync.Once
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

	el.ncClosed = make(chan struct{})
	conn, err := nats.Connect(common.Config.NatsUrl,
		nats.ClosedHandler(el.connectionClosed),
		// So the library gives up inside the same budget Close does.
		nats.DrainTimeout(CloseGrace))
	if err != nil {
		return nil, fmt.Errorf("nats.Connect: %w", err)
	}
	// After the error check, never before: nats.Connect returns a concrete
	// *nats.Conn, and assigning it to the interface field first would put a
	// typed nil there on failure — the trap this branch fixed in three other
	// places, reintroduced by making this field an interface.
	el.nc = conn

	// Every failure past this point closes the connection: returning (nil, err)
	// leaves it open with no reference to it, and the caller has nothing to
	// close.
	el.js, err = jetstream.New(conn)
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

// Close hands back what it can and stops, within CloseGrace.
//
// Order matters and none of it is obvious:
//
//  1. quit, so anything arriving from here on is handed back rather than queued
//     for a runner that is about to stop;
//  2. consumerCtx.Drain, which asks the consumer to push what it has already
//     prefetched through the callback instead of discarding it — Stop discards,
//     and those messages have their AckWait clock running;
//  3. wait for the runner, so no handler is still writing through the repo that
//     App.Shutdown closes next;
//  4. Nak whatever is still queued, because a callback can win the race in
//     handleMessage and enqueue after the runner has gone;
//  5. nc.Drain, and wait for it. This is the only wait that says anything about
//     the callbacks. Both Drain calls return immediately — the consumer's is a
//     flag and a closed channel — so counting callbacks in flight here counts
//     zero, which is what an earlier version of this function did. The
//     connection drain unsubscribes, waits for the handlers, flushes the Naks
//     they published and then closes, which is what ncClosed reports.
//
// Called from the fatal paths too, including the one where Run itself failed, so
// nothing here is guaranteed to exist. The queue is never closed: a callback may
// be blocked sending to it, and closing it under that is a panic on the NATS
// dispatch goroutine.
//
// One event can still be left to expire: a callback that passes the quit check
// in handleMessage, is preempted, and lands its send after step 4. It is
// unacknowledged, so it returns — after ackWait rather than at once.
func (el *EventListener) Close() {
	deadline := time.Now().Add(CloseGrace)

	if el.quit != nil {
		el.quitOnce.Do(func() { close(el.quit) })
	}
	if el.consumerCtx != nil {
		el.consumerCtx.Drain()
	}

	// Only if the runner is up: Consume failing means nothing will ever close
	// done, and waiting would hang the shutdown it was called to make orderly.
	if el.runnerStarted && el.done != nil {
		el.waitUntil("runner", el.done, deadline)
	}

	el.nakRemaining()

	if el.nc != nil {
		if err := el.nc.Drain(); err != nil {
			slog.Error("EventListener.Close nc.Drain", slog.Any("err", err))
			el.nc.Close()
			return
		}
		el.waitUntil("connection drain", el.ncClosed, deadline)
	}
}

// waitUntil waits on ch until deadline, reporting an overshoot rather than
// hanging the shutdown it is part of.
func (el *EventListener) waitUntil(what string, ch <-chan struct{}, deadline time.Time) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		slog.Warn("EventListener.Close out of time, continuing", slog.String("before", what))
		return
	}

	select {
	case <-ch:
	case <-time.After(remaining):
		slog.Warn("EventListener.Close gave up waiting, continuing",
			slog.String("on", what), slog.Duration("remaining", remaining))
	}
}

// CloseGrace bounds Close as a whole, not each wait inside it. Exported because
// the caller has to budget for it: api.App.Shutdown wraps this and its own, and
// per-wait grants had the caller funding one wait while Close spent several.
//
// It cannot be unbounded — the handlers reach the profile service and the
// connection drain waits on them — so one black-holed call would mean a
// shutdown that never finishes.
const CloseGrace = 6 * time.Second

// nakRemaining hands back anything still queued once the runner has stopped.
// Without it those events are neither handled nor returned until ackWait
// expires, which is minutes.
func (el *EventListener) nakRemaining() {
	for {
		select {
		case item := <-el.queue:
			if item.msg != nil {
				el.nak(item.msg)
			}
		default:
			return
		}
	}
}

func (el *EventListener) connectionClosed(*nats.Conn) {
	el.ncClosedOnce.Do(func() { close(el.ncClosed) })
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
		// Terminated rather than acked: Term says "never redeliver this",
		// which is what a payload that will never parse deserves, and it does
		// not depend on a delivery cap — MaxDeliver is unset, so a failed ack
		// here would come back every AckWait for ever, reporting to Sentry each
		// time. Without the return a zero-value Event went to the handlers, hit
		// the default branch of their type switch, and the corruption vanished.
		// The error is logged for form: TermWithReason needs a server at
		// 2.10.4 or above to honour the reason, and the client returns nil
		// either way, so a server that ignores it is not detectable here.
		// Every NATS declared in this repo is on 2.10.
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
	el.nak(msg)
}

func (el *EventListener) nak(msg jetstream.Msg) {
	if err := msg.Nak(); err != nil {
		slog.Error("EventListener nak", slog.Any("err", err))
	}
}
