package cmd

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func withKeycloakConfig(t *testing.T, url, realm, id, secret string) {
	t.Helper()

	// The value, not the pointer: common.Config is *envConfig, so saving the
	// pointer restores nothing and the last case here would leak into every
	// test that runs after it.
	saved := *common.Config
	t.Cleanup(func() { *common.Config = saved })

	common.Config.KeycloakServerUrl = url
	common.Config.KeycloakRealm = realm
	common.Config.KeycloakClientID = id
	common.Config.KeycloakClientSecret = secret
}

func TestKeycloakConfigErrorNamesTheMissingVariable(t *testing.T) {
	withKeycloakConfig(t, "https://kc.example", "vh", "orders", "s3cret")
	if err := keycloakConfigError(); err != nil {
		t.Fatalf("a complete configuration must pass: %v", err)
	}

	for _, missing := range []struct {
		name                   string
		url, realm, id, secret string
		wants                  string
	}{
		{"server url", "", "vh", "orders", "s3cret", "KEYCLOAK_SERVER_URL"},
		{"realm", "https://kc.example", "", "orders", "s3cret", "KEYCLOAK_REALM"},
		{"client id", "https://kc.example", "vh", "", "s3cret", "KEYCLOAK_CLIENT_ID"},
		{"client secret", "https://kc.example", "vh", "orders", "", "KEYCLOAK_CLIENT_SECRET"},
	} {
		t.Run(missing.name, func(t *testing.T) {
			withKeycloakConfig(t, missing.url, missing.realm, missing.id, missing.secret)

			err := keycloakConfigError()
			if err == nil {
				t.Fatalf("a missing %s must be an error", missing.name)
			}
			if !strings.Contains(err.Error(), missing.wants) {
				t.Fatalf("the error should name %s, got %q", missing.wants, err)
			}
		})
	}
}

// The charging commands must validate before they wire anything.
//
// A source check because the real thing is unreachable from a test: it exits the
// process, and the commands open a database. Two properties matter and neither
// is visible at a call site — that the check happens at all, and that it happens
// in the command rather than in buildChargeableBillingService, which runs after
// `defer cleanup()` and would therefore exit without draining.
//
// The credential used to be validated only by the muhlafim step, which
// retry-pricing-errors skips, --muhlafim=false disables, and an empty window
// returns before.
func TestTheChargingCommandsValidateBeforeWiringAnything(t *testing.T) {
	source, err := os.ReadFile("billing.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, fn := range []string{"runBillingStart", "runBillingRetryPricingErrors"} {
		body := regexp.MustCompile(`(?s)func ` + fn + `\([^)]*\)[^{]*\{(.*?)\n\}`).FindSubmatch(source)
		if body == nil {
			t.Errorf("%s not found in billing.go — has it been renamed?", fn)
			continue
		}

		validate := bytes.Index(body[1], []byte("validateChargeConfig()"))
		wire := bytes.Index(body[1], []byte("initBillingInfra()"))
		switch {
		case validate < 0:
			t.Errorf("%s must call validateChargeConfig()", fn)
		case wire >= 0 && validate > wire:
			t.Errorf("%s validates after initBillingInfra: a fatal then happens with the pool "+
				"open and NATS undrained", fn)
		}
	}
}

// And the builder must not exit, for the same reason: its callers have already
// deferred their cleanup by the time it runs.
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
		t.Error("buildChargeableBillingService must not exit: validate in the command instead")
	}
}
