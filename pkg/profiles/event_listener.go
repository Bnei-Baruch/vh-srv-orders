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

	"github.com/Bnei-Baruch/vh-srv-orders/common"
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

// natsConn is the part of *nats.Conn that Close uses, so Close is testable.
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

	// quit tells the callback and the runner to stop; the runner closes done
	// when it has. The queue is never closed: that would race the callback's send.
	quit     chan struct{}
	done     chan struct{}
	quitOnce sync.Once
	doneOnce sync.Once
	// ncClosed closes when the connection has drained: the only signal that
	// the callbacks are done, since both Drain calls are asynchronous.
	ncClosed     chan struct{}
	ncClosedOnce sync.Once
	// runnerStarted: whether anything will ever close done.
	runnerStarted bool

	// held is every delivered message not yet acked or naked: queued, being
	// handled, or blocked on a full queue. keepAlive extends their AckWait.
	heldMu sync.Mutex
	held   map[jetstream.Msg]struct{}
}

// ackWait is how long the server waits for an ack before redelivering, and so
// how long the events a dead instance held stay blocked: nothing Naks them when
// it crashes or is killed. It is short on purpose. The ack comes after the
// handlers, which can take longer (a queue of them, each up to two 30s
// profile calls), so keepAlive sends InProgress for every held message each
// progressInterval, and the server restarts their AckWait.
const ackWait = 30 * time.Second

// progressInterval leaves two missed heartbeats before AckWait runs out. A var
// so tests can shorten it.
var progressInterval = ackWait / 3

// queued carries the message with its event so the ack happens after the handlers.
type queued struct {
	event Event
	msg   jetstream.Msg
}

// queueCapacity is how many delivered events wait for the runner. It used to be
// written `2^10`, which is 2 XOR 10 = 8 in Go; 8 is kept on purpose. A full
// queue blocks the callback, which is backpressure.
const queueCapacity = 8

func NewEventListener() (*EventListener, error) {
	el := new(EventListener)

	el.ncClosed = make(chan struct{})
	conn, err := nats.Connect(common.Config.NatsUrl,
		nats.ClosedHandler(el.connectionClosed),
		// An abandoned drain is reported only here, and it loses the Naks just published.
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, natsErr error) {
			subject := ""
			if sub != nil {
				subject = sub.Subject
			}
			slog.Error("nats async error", slog.String("subject", subject), slog.Any("err", natsErr))
			sentry.CaptureException(natsErr)
		}),
		// Inside what Close has left when it gets to the drain, not all of CloseGrace.
		nats.DrainTimeout(connDrainTimeout))
	if err != nil {
		return nil, fmt.Errorf("nats.Connect: %w", err)
	}
	// After the error check: a nil *nats.Conn in the interface field is a typed nil.
	el.nc = conn

	// Failures from here on close the connection; the caller has nothing to close.
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
		// Set explicitly, as the stall bound after a crash; keepAlive covers
		// slow handlers. MaxDeliver is left unset on purpose: without a dead
		// letter, a cap silently discards the event on its last delivery.
		AckWait: ackWait,
		// Bounds delivered-but-unacked messages to what the queue holds;
		// PullMaxMessages does not (default 1000).
		MaxAckPending: queueCapacity,
	}
}

func (el *EventListener) Run() error {
	var err error
	// Prefetch bounded to the queue: delivered messages start their AckWait clock.
	el.consumerCtx, err = el.consumer.Consume(el.handleMessage,
		jetstream.PullMaxMessages(queueCapacity))
	if err != nil {
		return fmt.Errorf("jetstream consumer.Consume: %w", err)
	}

	el.runnerStarted = true
	go el.runQueue()
	go el.keepAlive()

	return nil
}

// keepAlive sends InProgress for every held message each progressInterval,
// until the runner stops. After that Close Naks what is left.
func (el *EventListener) keepAlive() {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			el.recovered("keepAlive", el.touchHeld)
		case <-el.done:
			return
		}
	}
}

func (el *EventListener) touchHeld() {
	el.heldMu.Lock()
	msgs := make([]jetstream.Msg, 0, len(el.held))
	for msg := range el.held {
		msgs = append(msgs, msg)
	}
	el.heldMu.Unlock()

	for _, msg := range msgs {
		if err := msg.InProgress(); err != nil {
			slog.Error("EventListener in-progress", slog.Any("err", err))
		}
	}
}

func (el *EventListener) hold(msg jetstream.Msg) {
	el.heldMu.Lock()
	defer el.heldMu.Unlock()
	if el.held == nil {
		el.held = make(map[jetstream.Msg]struct{})
	}
	el.held[msg] = struct{}{}
}

func (el *EventListener) release(msg jetstream.Msg) {
	el.heldMu.Lock()
	defer el.heldMu.Unlock()
	delete(el.held, msg)
}

// runQueue delivers queued events until quit, then drains the queue and closes
// done so Close can wait for it.
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

// deliver runs the handlers for one event, each recovered, then acks it. The
// ack comes after the handlers so a shutdown cannot swallow an unhandled event,
// and regardless of success: handlers report failure by logging, so a
// redelivery would just repeat it. (The old runner-wide recover restarted Run
// from this goroutine, racing Close; it is gone.)
func (el *EventListener) deliver(item queued) {
	defer func() {
		if item.msg == nil {
			return
		}
		el.release(item.msg)
		// Past Close's grace this can fail, and the event is redelivered.
		if err := item.msg.Ack(); err != nil {
			slog.Error("EventListener ack", slog.Any("err", err),
				slog.String("event_type", item.event.Type))
		}
	}()

	for _, handler := range el.handlers {
		el.callHandler(handler, item.event)
	}
}

// callHandler runs one handler, recovered, so a panic does not skip the rest.
func (el *EventListener) callHandler(handler EventHandler, event Event) {
	el.recovered("handler:"+event.Type, func() { handler(event) })
}

// recovered runs fn, reporting a panic instead of letting it kill the process
// and skip the drain.
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
			el.recovered("deliver", func() { el.deliver(item) })
		default:
			return
		}
	}
}

// Close hands back what it can and stops, within CloseGrace. Order matters:
//
//  1. close quit, so new arrivals are Nak'ed rather than queued;
//  2. consumerCtx.Drain, so prefetched messages go through the callback (and
//     get Nak'ed) instead of being discarded with their AckWait running;
//  3. wait for the runner, so no handler writes through the repo App.Shutdown
//     closes next;
//  4. Nak whatever a racing callback still queued;
//  5. nc.Drain and wait for the close: the only wait that covers the callbacks,
//     and it flushes the Naks.
//
// Called from fatal paths too, so nothing is guaranteed to exist. Known gap: a
// callback preempted between its quit check and its send can enqueue after
// step 4; that event returns after ackWait instead of at once. A crash or kill
// skips Close entirely, and everything held returns after ackWait.
func (el *EventListener) Close() {
	if el.quit != nil {
		el.quitOnce.Do(func() { close(el.quit) })
	}
	if el.consumerCtx != nil {
		el.consumerCtx.Drain()
	}

	// Only if the runner is up, or nothing will ever close done.
	if el.runnerStarted && el.done != nil {
		el.waitFor("runner", el.done, runnerShare)
	}

	el.nakRemaining()

	if el.nc != nil {
		if err := el.nc.Drain(); err != nil {
			slog.Error("EventListener.Close nc.Drain", slog.Any("err", err))
			el.nc.Close()
			return
		}
		el.waitFor("connection drain", el.ncClosed, connDrainTimeout+natsFlushTimeout)
	}
}

// waitFor waits on ch for at most grace. Each wait has its own share, so one
// slow step cannot spend the whole budget and leave the Naks unflushed.
func (el *EventListener) waitFor(what string, ch <-chan struct{}, grace time.Duration) {
	select {
	case <-ch:
	case <-time.After(grace):
		slog.Warn("EventListener.Close gave up waiting, continuing",
			slog.String("on", what), slog.Duration("grace", grace))
	}
}

// Close's budget. natsFlushTimeout is nats.go's fixed FlushTimeout(5s), run
// after the DrainTimeout-bounded subscription phase and before the close that
// fires ncClosed. CloseGrace is exported for the server's shutdown, which does
// not call Close yet at this commit (fix/server-graceful-shutdown wires it).
//
// runnerShare is less than one slow handler can take (two 30s profile calls),
// so with a slow profile service a handler can outlive the wait and meet a
// closed pool. Bounding the handlers themselves is outside this change.
const (
	natsFlushTimeout = 5 * time.Second
	connDrainTimeout = 2 * time.Second
	runnerShare      = 3 * time.Second

	CloseGrace = runnerShare + connDrainTimeout + natsFlushTimeout
)

// nakRemaining hands back anything still queued once the runner has stopped,
// rather than leaving it until ackWait expires.
func (el *EventListener) nakRemaining() {
	for {
		select {
		case item := <-el.queue:
			if item.msg != nil {
				el.release(item.msg)
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
		// Term, not Ack: it will never parse, and MaxDeliver is unset. Plain
		// Term rather than TermWithReason, which servers before 2.10.4 ignore
		// entirely, not just the reason.
		if termErr := msg.Term(); termErr != nil {
			slog.Error("EventListener.handleMessage term", slog.Any("err", termErr))
		}
		return
	}

	// Quit first on its own: with queue space both cases below are ready and
	// select picks at random.
	select {
	case <-el.quit:
		el.drop(msg)
		return
	default:
	}

	// Held from here: a full queue blocks this send, and the AckWait runs
	// while it does.
	el.hold(msg)
	select {
	case el.queue <- queued{event: event, msg: msg}:
	case <-el.quit:
		el.release(msg)
		el.drop(msg)
	}
}

// drop Naks an event during shutdown, so it comes back now rather than after ackWait.
func (el *EventListener) drop(msg jetstream.Msg) {
	slog.Debug("EventListener dropped an event during shutdown")
	el.nak(msg)
}

func (el *EventListener) nak(msg jetstream.Msg) {
	if err := msg.Nak(); err != nil {
		slog.Error("EventListener nak", slog.Any("err", err))
	}
}
