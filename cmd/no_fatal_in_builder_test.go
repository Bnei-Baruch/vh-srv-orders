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

	if bytes.Contains(body[1], []byte("LogFatal")) {
		t.Error("buildChargeableBillingService must not exit: it is called after `defer cleanup()`, " +
			"so a fatal here skips the drain. Validate in the command, before initBillingInfra, " +
			"or return an error")
	}
}
