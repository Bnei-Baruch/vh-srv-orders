package profiles

import (
	"sync/atomic"
	"testing"
	"time"
)

// Close is called from api.App.Shutdown, which the fatal paths reach — including
// the one where Run itself failed. None of the fields it touches exist then.
func TestCloseBeforeRun(t *testing.T) {
	new(EventListener).Close()
}

// Close must not return until the runner has stopped, because App.Shutdown
// closes the repo next and these handlers write through it. Before the
// handshake, Close only signalled: the runner kept draining buffered events
// against a closed pool.
//
// Buffered events are handled rather than dropped, since handleMessage acks on
// enqueue — dropping them would lose them permanently.
func TestCloseWaitsForTheRunnerAndDrainsWhatIsBuffered(t *testing.T) {
	listener := &EventListener{
		queue:         make(chan Event, 8),
		quit:          make(chan struct{}),
		done:          make(chan struct{}),
		runnerStarted: true,
	}

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
		listener.queue <- Event{}
	}

	listener.Close()
	stopped.Store(true)
	time.Sleep(20 * time.Millisecond)

	if handled.Load() != 4 {
		t.Errorf("handled %d of 4 buffered events — the rest were acked and lost", handled.Load())
	}
	if afterStop.Load() != 0 {
		t.Errorf("%d handlers ran after the runner reported done, so the repo could be closed "+
			"underneath them", afterStop.Load())
	}
}

// The delivery callback must never be able to send into a closed queue. Closing
// it under a blocked send panics on the NATS dispatch goroutine, which would
// kill the process mid-drain.
func TestCloseDoesNotCloseTheQueue(t *testing.T) {
	listener := &EventListener{
		queue: make(chan Event, 1),
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
	}

	listener.Close()

	select {
	case listener.queue <- Event{}:
	default:
		t.Fatal("the queue is full or closed after Close")
	}
}
