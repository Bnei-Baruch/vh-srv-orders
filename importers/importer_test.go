package importers

import "testing"

// doImport calls Close on the Init failure path, and Init creates the emitter
// before the repo — so Close runs with either field still nil. It panicked here
// before the guards, which turned a database outage into a crash that reported
// nothing.
func TestBaseImporterCloseAfterFailedInit(t *testing.T) {
	NewBaseImporter().Close()
}
