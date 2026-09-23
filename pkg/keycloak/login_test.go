package keycloak

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nerzal/gocloak/v13"
	"github.com/golang-jwt/jwt/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// A Keycloak stub, because the property it pins is not reachable otherwise: a
// successful login resets the consecutive-failure count. Calling clearFailures()
// directly proves only the setter — deleting the call from login() and refresh()
// left the package green, and without the reset failures accumulate over a
// process's lifetime rather than consecutively.
//
// gocloak decodes the token it receives, so the stub serves a real RS256 JWT and
// the matching JWKS.
type keycloakStub struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	logins   atomic.Int64
	failNext atomic.Bool
}

func newKeycloakStub(t *testing.T) *keycloakStub {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	stub := &keycloakStub{key: key}

	// Registered under /auth, which is what gocloak.SetLegacyWildFlySupport()
	// in NewClient makes the client ask for. The stub is reached through
	// NewClient (see client below) precisely so that option is on the path
	// under test: registering the modern paths here instead would leave the
	// package green with the option deleted, while every login in production
	// 404s — the Keycloak this service talks to serves the legacy prefix.
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/realms/"+testRealm+"/protocol/openid-connect/token",
		func(w http.ResponseWriter, r *http.Request) {
			stub.logins.Add(1)
			if stub.failNext.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			access, refresh := stub.sign(t), stub.sign(t)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  access,
				"refresh_token": refresh,
				"token_type":    "Bearer",
				"expires_in":    900,
			})
		})
	mux.HandleFunc("/auth/realms/"+testRealm+"/protocol/openid-connect/certs",
		func(w http.ResponseWriter, r *http.Request) {
			kid, kty, alg, use := "test-key", "RSA", "RS256", "sig"
			n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
			e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gocloak.CertResponse{
				Keys: &[]gocloak.CertResponseKey{{
					Kid: &kid, Kty: &kty, Alg: &alg, Use: &use, N: &n, E: &e,
				}},
			})
		})

	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)

	return stub
}

func (s *keycloakStub) sign(t *testing.T) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": time.Now().Add(15 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
		"azp": "test-client",
	})
	token.Header["kid"] = "test-key"

	signed, err := token.SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

const testRealm = "test-realm"

func (s *keycloakStub) client(t *testing.T) *Client {
	t.Helper()

	savedURL, savedRealm, savedID, savedSecret := common.Config.KeycloakServerUrl,
		common.Config.KeycloakRealm, common.Config.KeycloakClientID,
		common.Config.KeycloakClientSecret
	common.Config.KeycloakServerUrl = s.server.URL
	common.Config.KeycloakRealm = testRealm
	common.Config.KeycloakClientID = "test-client"
	common.Config.KeycloakClientSecret = "test-secret"
	t.Cleanup(func() {
		common.Config.KeycloakServerUrl = savedURL
		common.Config.KeycloakRealm = savedRealm
		common.Config.KeycloakClientID = savedID
		common.Config.KeycloakClientSecret = savedSecret
	})

	// NewClient, not a hand-built Client: everything it applies to the gocloak
	// client — the legacy /auth prefix, the request deadline — is then part of
	// what these tests exercise rather than something they quietly skip.
	return NewClient()
}

func TestSuccessfulLoginResetsTheFailureCount(t *testing.T) {
	stub := newKeycloakStub(t)
	client := stub.client(t)

	// Two failures, one short of the threshold.
	stub.failNext.Store(true)
	for i := 0; i < 2; i++ {
		if _, err := client.Token(); err == nil {
			t.Fatal("a 500 from the token endpoint should not yield a token")
		}
	}

	// A success in between has to wipe them, or the count is a lifetime tally.
	stub.failNext.Store(false)
	if _, err := client.Token(); err != nil {
		t.Fatalf("login against the stub should succeed: %v", err)
	}

	client.mu.Lock()
	failures := client.failures
	client.mu.Unlock()
	if failures != 0 {
		t.Fatalf("a successful login must reset the count, got %d", failures)
	}

	// And the proof that matters: after the reset, two more failures still do
	// not suppress, because the sequence counts as fail, fail rather than
	// four in a row. The cached token has to be expired first, or the fast
	// path returns it and Keycloak is never called at all.
	expired := jwt.MapClaims{"exp": float64(time.Now().Add(-time.Minute).Unix())}
	client.mu.Lock()
	client.claims = &expired
	client.mu.Unlock()

	stub.failNext.Store(true)
	for i := 0; i < 2; i++ {
		if _, err := client.Token(); err == nil {
			t.Fatal("expected failure once the cached token is expired")
		}
	}

	client.mu.Lock()
	failures, suppressing := client.failures, client.backingOff()
	client.mu.Unlock()

	if failures != 2 {
		t.Fatalf("the count should have restarted at the reset: got %d, want 2", failures)
	}
	if suppressing {
		t.Fatal("two consecutive failures after a success must not suppress")
	}
}

// The same for refresh: a token that expires is refreshed rather than
// re-logged-in, and that path resets the count too.
func TestSuccessfulRefreshResetsTheFailureCount(t *testing.T) {
	stub := newKeycloakStub(t)
	client := stub.client(t)

	// Log in once so there is a refresh token to use.
	if _, err := client.Token(); err != nil {
		t.Fatalf("initial login: %v", err)
	}

	// Expire the cached claims so the next call refreshes, and pre-load some
	// failures for the refresh to clear.
	expired := jwt.MapClaims{"exp": float64(time.Now().Add(-time.Minute).Unix())}
	client.mu.Lock()
	client.claims = &expired
	client.failures = 2
	client.lastFailure = time.Now()
	client.mu.Unlock()

	if _, err := client.Token(); err != nil {
		t.Fatalf("refresh against the stub should succeed: %v", err)
	}

	client.mu.Lock()
	failures := client.failures
	client.mu.Unlock()
	if failures != 0 {
		t.Fatalf("a successful refresh must reset the count, got %d", failures)
	}
}
