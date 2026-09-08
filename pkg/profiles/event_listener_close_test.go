package profiles

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// stubMsg is a delivered message that records its own acknowledgement. Only the
// three methods this package touches do anything.
type stubMsg struct {
	data string
	acks atomic.Int64
}

func (m *stubMsg) Data() []byte                              { return []byte(m.data) }
func (m *stubMsg) Ack() error                                { m.acks.Add(1); return nil }
func (m *stubMsg) Nak() error                                { return nil }
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
	listener.Close()
	waited := time.Since(start)

	if waited > drainGrace+2*time.Second {
		t.Fatalf("Close waited %v on a stuck handler", waited)
	}
	if waited < drainGrace {
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
