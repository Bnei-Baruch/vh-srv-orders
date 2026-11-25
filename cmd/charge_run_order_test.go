package cmd

import (
	"bytes"
	"os"
	"regexp"
	"testing"
)

// funcBody returns the source of a top-level function or method in billing.go.
func funcBody(t *testing.T, signature string) []byte {
	t.Helper()
	source, err := os.ReadFile("billing.go")
	if err != nil {
		t.Fatal(err)
	}
	body := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(signature) + `[^{]*\{(.*?)\n\}`).FindSubmatch(source)
	if body == nil {
		t.Fatalf("%s not found in billing.go — has it been renamed?", signature)
	}
	return body[1]
}

// Anything that can exit — the config check and every client constructor —
// must run before initBillingInfra, while there is nothing to drain. An exit
// after it would skip the callers' `defer cleanup()`: the pool stays open and
// NATS is dropped without draining.
func TestInitChargeRun_BuildsEverythingBeforeOpeningInfra(t *testing.T) {
	body := funcBody(t, "func initChargeRun(")

	infra := bytes.Index(body, []byte("initBillingInfra("))
	if infra < 0 {
		t.Fatal("initChargeRun no longer calls initBillingInfra")
	}
	for _, before := range []string{"validateChargeConfig(", "newChargeParts("} {
		at := bytes.Index(body, []byte(before))
		if at < 0 || at > infra {
			t.Errorf("initChargeRun must call %s before initBillingInfra", before)
		}
	}
}

// service is all that runs inside the cleanup, and it only assigns fields. A
// call that can exit belongs in newChargeParts or validateChargeConfig.
func TestChargeParts_ServiceOnlyWires(t *testing.T) {
	body := funcBody(t, "func (p chargeParts) service(")

	calls := regexp.MustCompile(`([\w.]+)\(`).FindAllSubmatch(body, -1)
	allowed := map[string]bool{
		"p.resolver.SetManualDiscountProvider": true,
		"p.resolver.SetHHGrantProvider":        true,
		"p.resolver.SetCouponProvider":         true,
		"billing.NewBillingService":            true,
	}
	for _, call := range calls {
		if !allowed[string(call[1])] {
			t.Errorf("chargeParts.service calls %s, which runs after initBillingInfra: "+
				"build it in newChargeParts instead, or add it here only if it cannot exit", call[1])
		}
	}
}
