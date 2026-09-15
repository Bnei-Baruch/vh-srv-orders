package cmd

import (
	"bytes"
	"os"
	"regexp"
	"testing"
)

// buildChargeableBillingService runs after its callers' `defer cleanup()`, so a
// fatal inside it skips the drain — the pool stays open and NATS is dropped
// without draining. Its configuration checks were hoisted into the commands for
// that reason, and this stops them coming back.
func TestTheChargeBuilderDoesNotFatal(t *testing.T) {
	source, err := os.ReadFile("billing.go")
	if err != nil {
		t.Fatal(err)
	}

	body := regexp.MustCompile(`(?s)func buildChargeableBillingService\([^)]*\)[^{]*\{(.*?)\n\}`).
		FindSubmatch(source)
	if body == nil {
		t.Fatal("buildChargeableBillingService not found in billing.go — has it been renamed?")
	}

	// Every way out, not just the one that was there when this was written: the
	// invariant is "this function does not exit", and FatalAfter, log.Fatal or a
	// bare os.Exit skip the drain exactly as LogFatal does.
	for _, exit := range []string{"LogFatal", "FatalAfter", "os.Exit", "log.Fatal"} {
		if bytes.Contains(body[1], []byte(exit)) {
			t.Errorf("buildChargeableBillingService calls %s: it runs after its callers' "+
				"`defer cleanup()`, so exiting here skips the drain. Validate in the command, "+
				"before initBillingInfra, or return an error", exit)
		}
	}
}
