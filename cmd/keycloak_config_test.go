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

// A source check because the real thing is unreachable from a test: it exits
// the process, and buildChargeableBillingService opens a database first. What
// matters is only that the call is there — the credential used to be validated
// by the muhlafim step alone, which retry-pricing-errors skips,
// --muhlafim=false disables, and an empty window returns before.
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
