package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

// buildChargeableBillingService runs after its callers' `defer cleanup()`, so a
// fatal inside it skips the drain — the pool stays open and NATS is dropped
// without draining. Its configuration checks were hoisted into the commands for
// that reason, and these tests stop them coming back.
//
// They catch the shape that was there: a missing-config check that exits.
// They do not prove the builder never exits. A fatal that fires only on a set
// or malformed value (an unparsable URL, say), or one reached through a
// function value such as `var die = utils.LogFatal`, passes both. Ruling those
// out needs a call-graph check, which these are not.

// Runs the builder in a child process, where an exit shows up as a failed
// child instead of killing the test binary, with common.Config zeroed
// (testutil's init loads .env into it). Every missing-config check fails
// there, so one that exits, in the builder or anything it calls, fails this.
func TestTheChargeBuilderReturnsWithEmptyConfig(t *testing.T) {
	if os.Getenv("CHARGE_BUILDER_CHILD") == "1" {
		config := reflect.ValueOf(common.Config).Elem()
		config.Set(reflect.Zero(config.Type()))
		for _, dryRun := range []bool{false, true} {
			if buildChargeableBillingService(nil, nil, dryRun) == nil {
				t.Fatal("no service built")
			}
		}
		os.Stderr.WriteString("BUILDER RETURNED\n")
		return
	}

	child := exec.Command(os.Args[0], "-test.run=^TestTheChargeBuilderReturnsWithEmptyConfig$")
	child.Env = append(os.Environ(), "CHARGE_BUILDER_CHILD=1")
	out, err := child.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "BUILDER RETURNED") {
		t.Fatalf("buildChargeableBillingService, or something it calls, exited instead of "+
			"returning; that skips its callers' `defer cleanup()`. Validate in the command, "+
			"before initBillingInfra, or return an error (%v):\n%s", err, out)
	}
}

// A fatal behind a condition the empty config does not reach is invisible to
// the test above, so the builder's own body is also checked as text.
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

	for _, exit := range []string{"LogFatal", "FatalAfter", "os.Exit", "log.Fatal"} {
		if bytes.Contains(body[1], []byte(exit)) {
			t.Errorf("buildChargeableBillingService calls %s: it runs after its callers' "+
				"`defer cleanup()`, so exiting here skips the drain. Validate in the command, "+
				"before initBillingInfra, or return an error", exit)
		}
	}
}
