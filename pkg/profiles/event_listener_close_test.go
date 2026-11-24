package profiles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// stubMsg is a delivered message that records its own acknowledgement. Only the
// three methods this package touches do anything.
type stubMsg struct {
	data      string
	acks      atomic.Int64
	naks      atomic.Int64
	ackPanics bool
}

func (m *stubMsg) Data() []byte { return []byte(m.data) }
func (m *stubMsg) Ack() error {
	m.acks.Add(1)
	if m.ackPanics {
		panic("ack exploded")
	}
	return nil
}
func (m *stubMsg) Nak() error                                { m.naks.Add(1); return nil }
func (m *stubMsg) NakWithDelay(time.Duration) error          { return nil }
func (m *stubMsg) DoubleAck(context.Context) error           { return nil }
func (m *stubMsg) InProgress() error                         { return nil }
func (m *stubMsg) Term() error                               { return nil }
func (m *stubMsg) TermWithReason(string) error               { return nil }
func (m *stubMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *stubMsg) Headers() nats.Header                      { return nil }
func (m *stubMsg) Subject() string                           { return "" }
func (m *stubMsg) Reply() string                             { return "" }

// Compile-time check that the stub still satisfies what the listener is handed.
var _ jetstream.Msg = (*stubMsg)(nil)

func newListener() *EventListener {
	return &EventListener{
		queue:         make(chan queued, queueCapacity),
		quit:          make(chan struct{}),
		done:          make(chan struct{}),
		ncClosed:      make(chan struct{}),
		runnerStarted: true,
	}
}

// Close is called from api.App.Shutdown, which the fatal paths reach — including
// the one where Run itself failed. None of the fields it touches exist then.
func TestCloseBeforeRun(t *testing.T) {
	new(EventListener).Close()
}

// Close must not return until the runner has stopped, because App.Shutdown
// closes the repo next and these handlers write through it.
func TestCloseWaitsForTheRunnerAndDrainsWhatIsBuffered(t *testing.T) {
	listener := newListener()

	var handled, afterStop atomic.Int64
	var stopped atomic.Bool
	listener.RegisterHandler(func(Event) {
		if stopped.Load() {
			afterStop.Add(1)
		}
		handled.Add(1)
	})

	go listener.runQueue()
	for i := 0; i < 4; i++ {
		listener.queue <- queued{event: Event{}}
	}

	listener.Close()
	stopped.Store(true)
	time.Sleep(20 * time.Millisecond)

	if handled.Load() != 4 {
		t.Errorf("handled %d of 4 buffered events", handled.Load())
	}
	if afterStop.Load() != 0 {
		t.Errorf("%d handlers ran after Close returned, so the repo could be closed underneath them",
			afterStop.Load())
	}
}

// The runner's wait is bounded, and bounded by its own share rather than by the
// whole of Close.
func TestCloseGivesUpOnAStuckRunner(t *testing.T) {
	listener := newListener()

	release := make(chan struct{})
	listener.RegisterHandler(func(Event) { <-release })
	defer close(release)

	go listener.runQueue()
	listener.queue <- queued{event: Event{}}
	time.Sleep(30 * time.Millisecond) // let the handler block

	start := time.Now()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		listener.Close()
	}()

	// Bounded here too, so a regression reports itself instead of hanging until
	// the package times out and prints a goroutine dump.
	select {
	case <-closed:
	case <-time.After(CloseGrace + 5*time.Second):
		t.Fatalf("Close did not return within %v of its budget: the wait is unbounded", CloseGrace)
	}

	waited := time.Since(start)
	if waited < runnerShare {
		t.Fatalf("Close returned after %v, before the runner's own share of %v — the wait is "+
			"not happening", waited, runnerShare)
	}
	if waited > runnerShare+3*time.Second {
		t.Fatalf("Close spent %v on a runner allowed %v: the wait is against the whole budget "+
			"rather than its share", waited, runnerShare)
	}
}

// Nothing is acknowledged until a handler has run: an event acked without being
// handled is neither redelivered nor processed.
func TestTheAckFollowsTheHandlers(t *testing.T) {
	listener := newListener()

	var ackedDuringHandler atomic.Bool
	msg := &stubMsg{data: `{"type":"update_profile"}`}
	listener.RegisterHandler(func(Event) {
		ackedDuringHandler.Store(msg.acks.Load() > 0)
	})

	go listener.runQueue()
	listener.queue <- queued{event: Event{}, msg: msg}
	listener.Close()

	if ackedDuringHandler.Load() {
		t.Error("the message was acked before the handler ran")
	}
	if msg.acks.Load() != 1 {
		t.Errorf("acked %d times, want once", msg.acks.Load())
	}
}

// A handler panic costs one event, not the listener.
func TestAHandlerPanicDoesNotStopTheRunner(t *testing.T) {
	listener := newListener()

	var handled atomic.Int64
	listener.RegisterHandler(func(e Event) {
		handled.Add(1)
		if e.Type == "boom" {
			panic("handler exploded")
		}
	})

	go listener.runQueue()
	poison := &stubMsg{}
	listener.queue <- queued{event: Event{Type: "boom"}, msg: poison}
	listener.queue <- queued{event: Event{Type: "fine"}}
	listener.Close()

	if handled.Load() != 2 {
		t.Errorf("handled %d events, want 2 — the panic stopped the runner", handled.Load())
	}
	if poison.acks.Load() != 1 {
		t.Errorf("the event that panicked was acked %d times, want once: unacked it would be "+
			"redelivered and panic again for ever", poison.acks.Load())
	}
}

// The delivery callback must never be able to send into a closed queue.
func TestCloseDoesNotCloseTheQueue(t *testing.T) {
	listener := &EventListener{
		queue: make(chan queued, 1),
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
	}

	listener.Close()

	select {
	case listener.queue <- queued{}:
	default:
		t.Fatal("the queue is full or closed after Close")
	}
}

// One handler panicking must not cost the others their event.
func TestAPanicInOneHandlerDoesNotSkipTheNext(t *testing.T) {
	listener := newListener()

	var second atomic.Int64
	listener.RegisterHandler(func(Event) { panic("first handler exploded") })
	listener.RegisterHandler(func(Event) { second.Add(1) })

	go listener.runQueue()
	msg := &stubMsg{}
	listener.queue <- queued{event: Event{Type: "update_profile"}, msg: msg}
	listener.Close()

	if second.Load() != 1 {
		t.Errorf("the second handler ran %d times, want once", second.Load())
	}
	if msg.acks.Load() != 1 {
		t.Errorf("acked %d times, want once", msg.acks.Load())
	}
}

// The consumer sets an ack window long enough for the queue: the ack
// waits for the handlers.
func TestConsumerBoundsItsRedelivery(t *testing.T) {
	config := consumerConfig()

	if config.AckWait <= 0 {
		t.Error("AckWait unset: the server's 30s applies, and a queued event can outlast it")
	}
	if config.AckWait < time.Minute {
		t.Errorf("AckWait is %v, which a handler with no deadline of its own can exceed", config.AckWait)
	}
	// MaxDeliver is deliberately unset: capping deliveries without something
	// consuming the MAX_DELIVERIES advisory discards the message instead of
	// containing it, and a lost create_profile is worse than a visible retry.
	if config.MaxDeliver != 0 {
		t.Errorf("MaxDeliver is %d, which drops the message on that delivery — there is no dead "+
			"letter to catch it", config.MaxDeliver)
	}
}

// The prefetch has to be bounded to the queue.
func TestTheConsumeCallBoundsItsPrefetch(t *testing.T) {
	source, err := os.ReadFile("event_listener.go")
	if err != nil {
		t.Fatal(err)
	}

	consume := regexp.MustCompile(`(?s)el\.consumer\.Consume\((.*?)\)\n`).FindSubmatch(source)
	if consume == nil {
		t.Fatal("the Consume call was not found in event_listener.go")
	}

	if !bytes.Contains(consume[1], []byte("jetstream.PullMaxMessages(queueCapacity)")) {
		t.Error("Consume must bound its prefetch with jetstream.PullMaxMessages(queueCapacity): " +
			"the default is 500 messages, all of them counting against AckWait while they wait " +
			"for a runner that handles them one at a time")
	}
}

// The runner has to survive a panic anywhere it goes, not only inside a
// handler.
func TestARunnerPanicOutsideAHandlerDoesNotKillTheProcess(t *testing.T) {
	listener := newListener()

	var handled atomic.Int64
	listener.RegisterHandler(func(Event) { handled.Add(1) })

	go listener.runQueue()
	listener.queue <- queued{event: Event{}, msg: &stubMsg{ackPanics: true}}
	listener.queue <- queued{event: Event{}, msg: &stubMsg{}}
	listener.Close()

	if handled.Load() != 2 {
		t.Errorf("handled %d events, want 2 — the panic in the ack stopped the runner", handled.Load())
	}
}

// An event dropped because shutdown began is naked, not just left silent.
func TestADroppedEventIsNakedForPromptRedelivery(t *testing.T) {
	listener := newListener()
	listener.quitOnce.Do(func() { close(listener.quit) })

	msg := &stubMsg{data: `{"type":"update_profile"}`}
	listener.handleMessage(msg)

	if msg.naks.Load() != 1 {
		t.Errorf("naked %d times, want once — it waits out AckWait instead", msg.naks.Load())
	}
	if msg.acks.Load() != 0 {
		t.Errorf("acked %d times: nothing handled it", msg.acks.Load())
	}
}

// The profile client's timeout is what the listener's drain rests on: an
// unbounded call turns Close's wait into the whole grace, every time.
func TestTheProfileClientIsBounded(t *testing.T) {
	timeout := NewProfileServiceAPI(nil).client.GetClient().Timeout

	if timeout == 0 {
		t.Error("the profile client has no timeout, so a handler can outlast any shutdown grace")
	}
}

// AckWait covers the queue, and what makes that possible is the cap on
// delivered-unacknowledged messages.
func TestTheConsumerCapsWhatCanBeWaitingForAnAck(t *testing.T) {
	config := consumerConfig()

	if config.MaxAckPending != queueCapacity {
		t.Errorf("MaxAckPending is %d, want %d: ackWait is derived against that number, and "+
			"unset means the server's 1000", config.MaxAckPending, queueCapacity)
	}

	// And the derivation itself, stated against the cap rather than against
	// ackWait's own definition.
	perEvent := 2 * requestTimeout
	if ackWait < time.Duration(config.MaxAckPending)*perEvent {
		t.Errorf("ackWait %v does not cover %d events at %v each: events still queued get "+
			"redelivered, and each redelivery publishes its own derived account event",
			ackWait, config.MaxAckPending, perEvent)
	}
}

// stubConn records what Close does with the connection, and lets the test
// decide when the drain reports itself finished.
type stubConn struct {
	drains   atomic.Int64
	closes   atomic.Int64
	drained  chan struct{}
	drainErr error
}

func (c *stubConn) Drain() error {
	c.drains.Add(1)
	if c.drained != nil {
		close(c.drained)
	}
	return c.drainErr
}

func (c *stubConn) Close() { c.closes.Add(1) }

// Close drains the connection and waits for it, rather than closing it
// outright.
func TestCloseDrainsTheConnectionAndWaitsForIt(t *testing.T) {
	listener := newListener()
	listener.runnerStarted = false
	conn := &stubConn{drained: make(chan struct{})}
	listener.nc = conn

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		listener.Close()
	}()

	// It has to ask for the drain...
	select {
	case <-conn.drained:
	case <-time.After(2 * time.Second):
		t.Fatal("Close never asked the connection to drain")
	}

	// ...and then wait for it to report itself finished.
	select {
	case <-returned:
		t.Fatal("Close returned without waiting for the drain to finish")
	case <-time.After(200 * time.Millisecond):
	}

	listener.connectionClosed(nil)
	select {
	case <-returned:
	case <-time.After(CloseGrace + 2*time.Second):
		t.Fatal("Close did not return once the drain reported closed")
	}

	if conn.closes.Load() != 0 {
		t.Errorf("the connection was closed outright %d times, which skips the drain",
			conn.closes.Load())
	}
}

// A drain that cannot be started is not a reason to leave the connection open.
func TestCloseFallsBackToClosingWhenTheDrainFails(t *testing.T) {
	listener := newListener()
	listener.runnerStarted = false
	conn := &stubConn{drainErr: errors.New("connection closed")}
	listener.nc = conn

	listener.Close()

	if conn.closes.Load() != 1 {
		t.Errorf("closed %d times after a failed drain, want once", conn.closes.Load())
	}
}

// Close as a whole stays within CloseGrace.
func TestCloseIsBoundedAsAWhole(t *testing.T) {
	listener := newListener()
	listener.RegisterHandler(func(Event) { time.Sleep(time.Hour) })
	listener.nc = &stubConn{}

	go listener.runQueue()
	listener.queue <- queued{event: Event{}}
	time.Sleep(50 * time.Millisecond) // let the handler start

	start := time.Now()
	listener.Close()
	waited := time.Since(start)

	if waited > CloseGrace+2*time.Second {
		t.Fatalf("Close took %v against a %v grace: its waits are being spent one after another",
			waited, CloseGrace)
	}
	if waited < CloseGrace/2 {
		t.Fatalf("Close returned after %v, so nothing was waited on", waited)
	}
}

// Anything still queued when the runner stops is handed back, not left to
// expire.
func TestCloseNaksWhatIsStillQueued(t *testing.T) {
	listener := newListener()
	listener.runnerStarted = false

	stragglers := []*stubMsg{{}, {}}
	for _, msg := range stragglers {
		listener.queue <- queued{event: Event{}, msg: msg}
	}

	listener.Close()

	for i, msg := range stragglers {
		if msg.naks.Load() != 1 {
			t.Errorf("straggler %d naked %d times, want once — it waits out ackWait instead",
				i, msg.naks.Load())
		}
	}
}

// Close must Drain the consumer, not Stop it.
func TestCloseDrainsTheConsumerRatherThanStoppingIt(t *testing.T) {
	source, err := os.ReadFile("event_listener.go")
	if err != nil {
		t.Fatal(err)
	}

	closeBody := regexp.MustCompile(`(?s)func \(el \*EventListener\) Close\(\) \{(.*?)\n\}`).
		FindSubmatch(source)
	if closeBody == nil {
		t.Fatal("Close was not found in event_listener.go")
	}

	if !bytes.Contains(closeBody[1], []byte("el.consumerCtx.Drain()")) {
		t.Error("Close must call consumerCtx.Drain(): Stop discards the prefetched buffer, so " +
			"those events wait out ackWait instead of being naked back immediately")
	}
	if bytes.Contains(closeBody[1], []byte("el.consumerCtx.Stop()")) {
		t.Error("Close calls consumerCtx.Stop(), which discards the prefetched buffer")
	}
}

// The connection is published to its interface field only after the error is
// checked.
func TestTheConnectionIsPublishedOnlyOnSuccess(t *testing.T) {
	source, err := os.ReadFile("event_listener.go")
	if err != nil {
		t.Fatal(err)
	}

	assign := bytes.Index(source, []byte("el.nc = conn"))
	check := bytes.Index(source, []byte(`return nil, fmt.Errorf("nats.Connect`))
	switch {
	case assign < 0:
		t.Fatal("the connection assignment was not found: check this test before the code")
	case check < 0:
		t.Fatal("nats.Connect's error branch was not found")
	case assign < check:
		t.Error("el.nc is assigned before nats.Connect's error is checked, which leaves a typed " +
			"nil in an interface field on failure")
	}
}

// A slow runner must not spend the connection drain's share.
func TestASlowRunnerDoesNotSpendTheDrainsGrace(t *testing.T) {
	listener := newListener()
	listener.RegisterHandler(func(Event) { time.Sleep(time.Hour) })
	conn := &stubConn{}
	listener.nc = conn

	go listener.runQueue()
	listener.queue <- queued{event: Event{}}
	time.Sleep(50 * time.Millisecond) // let the handler block

	start := time.Now()
	listener.Close()
	waited := time.Since(start)

	// The runner overshoots its share, and the drain still gets asked for and
	// waited on afterwards.
	if conn.drains.Load() != 1 {
		t.Errorf("the connection was drained %d times: the runner consumed the drain's turn",
			conn.drains.Load())
	}
	if waited < runnerShare+connDrainTimeout {
		t.Errorf("Close returned after %v, which is less than the runner's %v plus the drain's "+
			"own wait — one of them was skipped", waited, runnerShare)
	}
	if waited > CloseGrace+2*time.Second {
		t.Errorf("Close took %v against a %v budget", waited, CloseGrace)
	}
}
