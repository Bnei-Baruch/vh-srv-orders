package events

import (
	"sync"
	"testing"
)

// Billing workers emit events concurrently. Run with -race (as CI does): it
// fails on a shared unsynchronized entropy source.
func TestMakeEvent_ConcurrentCallsGetDistinctIDs(t *testing.T) {
	const workers, each = 8, 500
	ids := make(chan string, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range each {
				ids <- MakeEvent(TypeUpdateOrder, nil).ID
			}
		})
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]bool, workers*each)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate event id %s", id)
		}
		seen[id] = true
	}
}
