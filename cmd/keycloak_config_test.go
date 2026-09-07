package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func withKeycloakConfig(t *testing.T, url, realm, id, secret string) {
	t.Helper()

	saved := common.Config
	t.Cleanup(func() { common.Config = saved })

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

// Charging authenticates to external_payments, so a charge command that does
// not check the credential first is the failure this pins.
//
// It is a source check because the alternative is not reachable from a test:
// the check exits the process, and buildChargeableBillingService opens a
// database and builds five clients before returning. What matters is not how
// the call is written but that it is there at all — the credential used to be
// validated only by the muhlafim step, which `retry-pricing-errors` skips,
// `--muhlafim=false` disables, and ProcessMuhlafim itself returns early from
// when no order is flagged. Missing it is not a startup failure: processOrder
// writes its pending payment row before the gateway call, so every order in the
// run is stored, finalised unsuccessful and reported to Sentry, with nothing
// charged and the whole run to redo.
func TestTheChargeBuilderValidatesTheCredential(t *testing.T) {
	source, err := os.ReadFile("billing.go")
	if err != nil {
		t.Fatal(err)
	}

	body := regexp.MustCompile(`(?s)func buildChargeableBillingService\([^)]*\)[^{]*\{(.*?)\n\}`).
		FindSubmatch(source)
	if body == nil {
		t.Fatal("buildChargeableBillingService not found in billing.go — has it been renamed?")
	}

	if !strings.Contains(string(body[1]), "validateKeycloakConfig()") {
		t.Error("buildChargeableBillingService must call validateKeycloakConfig(): every command " +
			"that charges authenticates to external_payments, and the muhlafim step's own check " +
			"is skipped by retry-pricing-errors, by --muhlafim=false, and by an empty window")
	}
}
