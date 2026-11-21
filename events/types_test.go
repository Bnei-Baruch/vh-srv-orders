package events

import (
	"sync"
	"testing"
)

// Billing workers emit events concurrently. Under -race this fails on a shared
// unsynchronized entropy source; without -race it still catches duplicate IDs.
func TestMakeEvent_ConcurrentCallsGetDistinctIDs(t *testing.T) {
	const workers, each = 8, 500
	ids := make(chan string, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				ids <- MakeEvent(TypeUpdateOrder, nil).ID
			}
		}()
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
