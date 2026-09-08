package cmd

import "testing"

// Same as importers.TestBaseImporterCloseAfterFailedInit: Do calls Close on the
// Init failure path, where repo and eventEmitter are still nil.
func TestWorkerCloseAfterFailedInit(t *testing.T) {
	NewWorker().Close()
}
