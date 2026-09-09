package profiles

import (
	"bytes"
	"context"
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
		runnerStarted: true,
	}
}

// Close is called from api.App.Shutdown, which the fatal paths reach — including
// the one where Run itself failed. None of the fields it touches exist then.
func TestCloseBeforeRun(t *testing.T) {
	new(EventListener).Close()
}

// Close must not return until the runner has stopped, because App.Shutdown
// closes the repo next and these handlers write through it. Before the
// handshake, Close only signalled: the runner kept delivering against a closed
// pool.
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

// The wait has to be bounded. The handlers reach the profile service over HTTP
// with no deadline, so an unbounded wait turns one black-holed connection into a
// shutdown that never finishes — and on a fatal path, a process that never
// exits.
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
		t.Fatalf("Close did not return within %v of its own grace: the wait is unbounded",
			5*time.Second)
	}

	waited := time.Since(start)
	if waited < CloseGrace {
		t.Fatalf("Close returned after %v, before its own grace — the wait is not happening", waited)
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

// A handler panic costs one event, not the listener. It used to take down the
// runner, which restarted itself from its own goroutine — writing state Close
// reads, and closing done before the replacement existed, so every later Close
// stopped waiting.
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

// One handler panicking must not cost the others their event. Invisible today,
// since App registers exactly one — but RegisterHandler appends, so the second
// one added would silently inherit "whatever the first panicked on, you do not
// see".
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

// The consumer's ack window and delivery cap are load-bearing now that the ack
// waits for the handlers: the server's defaults are 30s and unlimited, which
// turns a backlog into endless redelivery.
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

// The prefetch has to be bounded to the queue. Consume's default is 500
// messages, every one of which starts its AckWait on delivery while the runner
// works through them queueCapacity at a time — which is what turned a backlog
// into redelivery once the ack moved after the handlers.
//
// A source check because a PullConsumeOpt is a function this package cannot
// inspect from outside jetstream.
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

// The runner has to survive a panic anywhere it goes, not only inside a handler.
// deliver's deferred ack and drainQueue both run outside callHandler, and a
// panic there leaves the goroutine and takes the process down — skipping the
// drain this branch exists to perform. The self-restart removed earlier was
// catching these by accident.
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
// Unacknowledged it would come back anyway, but only after AckWait — two
// minutes, chosen so a handler still working is not redelivered underneath
// itself. Nothing is working on a dropped event.
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

// AckWait has to cover the queue, not one handler: the ack waits for the
// handlers, so the last prefetched message waits for everything ahead of it.
func TestAckWaitCoversTheWholeQueue(t *testing.T) {
	worstCase := time.Duration(queueCapacity) * 2 * requestTimeout

	if ackWait <= worstCase {
		t.Errorf("ackWait %v does not cover %d queued handlers at %v each with a retry (%v): "+
			"events still sitting in the queue get redelivered, and each redelivery publishes "+
			"its own derived account event", ackWait, queueCapacity, requestTimeout, worstCase)
	}
}

// Close waits for the connection drain, which is the only signal that the
// delivery callbacks are done.
//
// Both Drain calls return immediately — the consumer's is a flag and a closed
// channel — so an earlier version counted callbacks in flight at this point,
// counted zero, and closed the connection while the drained messages were still
// on their way to the callback. Those events then waited out ackWait, which is
// the outcome draining was meant to avoid.
func TestCloseWaitsForTheConnectionDrain(t *testing.T) {
	listener := newListener()
	listener.runnerStarted = false
	listener.ncClosed = make(chan struct{})

	// No connection, so drive the wait the way Close does once nc.Drain has
	// been asked for: it returns only when the drain reports itself closed.
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		listener.waitUntil("connection drain", listener.ncClosed, time.Now().Add(CloseGrace))
	}()

	select {
	case <-returned:
		t.Fatal("the wait returned before the drain reported closed")
	case <-time.After(200 * time.Millisecond):
	}

	listener.connectionClosed(nil)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("the wait did not return once the drain reported closed")
	}
}

// Anything still queued when the runner stops is handed back, not left to
// expire: a callback can pass handleMessage's quit check and land its send after
// the runner has gone.
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

// Close must Drain the consumer, not Stop it. Stop discards whatever has
// already been prefetched — up to queueCapacity events whose AckWait clock is
// running — so they are neither handled nor handed back, and come back only
// when that wait expires. Drain pushes them through the callback, where the
// closed quit turns each into a Nak.
//
// A source check: reaching this needs a live JetStream consumer.
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
