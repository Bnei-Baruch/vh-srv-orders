package profiles

import "testing"

// Close is called from api.App.Shutdown, which the fatal paths reach — including
// the one where Run itself failed. None of the fields it touches exist then.
func TestCloseBeforeRun(t *testing.T) {
	new(EventListener).Close()
}
