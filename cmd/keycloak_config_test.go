package cmd

import (
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

func withChargeConfig(t *testing.T) {
	t.Helper()
	withKeycloakConfig(t, "https://kc.example", "vh", "orders", "s3cret")
	// withKeycloakConfig restores the whole config, these included.
	common.Config.PriorityBaseURL = "https://priority.example"
	common.Config.AccountingServiceUrl = "https://accounting.example"
	common.Config.QuickbooksCompanyID = "qb"
}

// The charging commands exit on chargeConfigError before they wire anything,
// so it has to cover the credential as well as pricing: retry-pricing-errors
// skips the muhlafim step, the only other place the credential was checked.
func TestChargeConfigErrorPassesACompleteConfig(t *testing.T) {
	withChargeConfig(t)

	if err := chargeConfigError(); err != nil {
		t.Fatalf("a complete configuration must pass: %v", err)
	}
}

func TestChargeConfigErrorNamesAMissingKeycloakSecret(t *testing.T) {
	withChargeConfig(t)
	common.Config.KeycloakClientSecret = ""

	err := chargeConfigError()
	if err == nil || !strings.Contains(err.Error(), "KEYCLOAK_CLIENT_SECRET") {
		t.Fatalf("a missing client secret must be named, got %v", err)
	}
}

func TestChargeConfigErrorNamesAMissingPricingVariable(t *testing.T) {
	withChargeConfig(t)
	common.Config.PriorityBaseURL = ""

	err := chargeConfigError()
	if err == nil || !strings.Contains(err.Error(), "PRIORITY_BASE_URL") {
		t.Fatalf("a missing pricing variable must be named, got %v", err)
	}
}
