package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
)

// TEMPORARY — delete with checkout's /auth/whoami once /vh/* carries real
// traffic. Without it the first test of the Keycloak chain would be a payment,
// where a failure looks the same as a bad terminal or a Pelecard error.
//
//	go run . checkout auth-check
var checkoutCmd = &cobra.Command{
	Use:   "checkout",
	Short: "checkout (external_payments) commands",
	Long:  "Commands for talking to checkout, the service that holds the Pelecard credentials.",
}

var checkoutAuthCheckCmd = &cobra.Command{
	Use:   "auth-check",
	Short: "Verify that this service can authenticate to checkout via Keycloak",
	Long: "Obtains a Keycloak token as this service's client and calls checkout's " +
		"/auth/whoami with it. Makes no payment and writes nothing.",
	Run: checkoutAuthCheckFn,
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
	checkoutCmd.AddCommand(checkoutAuthCheckCmd)

	checkoutAuthCheckCmd.Flags().String("url", common.CheckoutBaseURL,
		"checkout base URL, for testing against a different deployment")
}

func checkoutAuthCheckFn(cmd *cobra.Command, args []string) {
	baseURL, _ := cmd.Flags().GetString("url")
	baseURL = strings.TrimRight(baseURL, "/")

	// Reported one link at a time, because "it does not work" is the least
	// useful outcome this command could produce.
	fmt.Printf("keycloak   %s realm=%s\n", common.Config.KeycloakServerUrl, common.Config.KeycloakRealm)
	fmt.Printf("client     %s\n", common.Config.KeycloakClientID)
	fmt.Printf("checkout   %s\n\n", baseURL)

	if common.Config.KeycloakClientID == "" || common.Config.KeycloakClientSecret == "" {
		fmt.Println("FAIL  KEYCLOAK_CLIENT_ID / KEYCLOAK_CLIENT_SECRET are not set")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// This service's own Keycloak client. checkout identifies callers by the azp
	// claim, so its api_clients row is keyed on this client id — a dedicated
	// payments identity would only buy independent rotation, and switching to
	// one later is a config value here and one CLI command there.
	token := keycloak.NewClient().AccessToken(ctx)
	if token == "" {
		fmt.Println("FAIL  could not obtain a token from Keycloak")
		fmt.Println("      the client id/secret, or the client is not configured for service accounts")
		os.Exit(1)
	}
	fmt.Printf("OK    obtained a token (%d chars)\n", len(token))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/auth/whoami", nil)
	if err != nil {
		fmt.Printf("FAIL  building the request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("FAIL  calling checkout: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println("OK    checkout accepted the token")
	case http.StatusUnauthorized:
		fmt.Println("FAIL  checkout rejected the token (401)")
		fmt.Println("      either it cannot verify it — KEYCLOAK_SERVER_URL/REALM there, or a")
		fmt.Println("      different realm — or no api_clients row matches this client id")
		fmt.Printf("      body: %s\n", strings.TrimSpace(string(body)))
		os.Exit(1)
	default:
		fmt.Printf("FAIL  checkout answered %d\n      body: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		os.Exit(1)
	}

	var who struct {
		Client         string `json:"client"`
		Organization   string `json:"organization"`
		Prefix         string `json:"prefix"`
		KeycloakReady  bool   `json:"keycloak_ready"`
		KeycloakIssuer string `json:"keycloak_issuer"`
	}
	if err = json.Unmarshal(body, &who); err != nil {
		fmt.Printf("      unexpected body: %s\n", strings.TrimSpace(string(body)))
		os.Exit(1)
	}

	fmt.Printf("\ncheckout sees:\n")
	fmt.Printf("  client        %s\n", who.Client)
	fmt.Printf("  organization  %s\n", who.Organization)
	fmt.Printf("  prefix        %s\n", who.Prefix)
	fmt.Printf("  issuer        %s\n", who.KeycloakIssuer)

	// The organization decides which terminal a charge lands on, so a token that
	// authenticates but resolves to the wrong one is worse than a rejected token
	// — it would work, and charge the wrong entity.
	if who.Organization != "ben2" {
		fmt.Printf("\nWARNING  expected organization ben2, got %q\n", who.Organization)
		os.Exit(1)
	}
	fmt.Println("\nall four links OK: keycloak reachable, token issued, accepted, mapped to ben2")
}
