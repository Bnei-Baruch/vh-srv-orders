package pelecardtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// stubTokens stands in for Keycloak. A fixed token when it should succeed, an
// error when the point of the test is that the call never leaves the process.
type stubTokens struct {
	token string
	err   error
}

func (s stubTokens) Token() (string, error) { return s.token, s.err }
func (s stubTokens) Invalidate()            {}

// withExternalPayments starts a stub external_payments and returns a client
// pointed at it, authenticating with the given token. Everything is restored
// afterwards.
func withExternalPayments(t *testing.T, token string, handler http.HandlerFunc) *pelecard.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := newChargeClient(t)
	client.BaseURL = server.URL
	client.Tokens = stubTokens{token: token}
	return client
}

func TestFetchMuhlafim_SendsTokenAndParsesEntries(t *testing.T) {
	var authHeader, path string
	var body pelecard.ExternalMuhlafimRequest

	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		path = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tok1": {"Token":"tok1","ActionDescription":"חיוב נקלט","NewCardNumber":"1234","NewExpirationDate":"0130"},
			"tok2": {"Token":"tok2","ActionDescription":"נדחה לא יחויב"}
		}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	assert.Equal(t, "Bearer tok_secret", authHeader)
	assert.Equal(t, "/token/muhlafim", path)
	assert.Equal(t, "21/08/2025 00:00", body.StartDate)
	assert.Equal(t, "24/09/2025 00:00", body.EndDate)

	require.Len(t, entries, 2)
	assert.Equal(t, "1234", entries["tok1"].NewCardNumber)
	assert.Equal(t, "0130", entries["tok1"].NewExpirationDate)
	assert.Equal(t, "נדחה לא יחויב", entries["tok2"].ActionDescription)
}

// The request carries no terminal and no credentials — that is the whole point
// of external_payments owning the call.
func TestFetchMuhlafim_SendsNoCredentials(t *testing.T) {
	var raw map[string]any

	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	for _, forbidden := range []string{"user", "password", "terminalNumber", "TerminalNumber"} {
		assert.NotContains(t, raw, forbidden)
	}
}

// The shared client also posts directly to Pelecard while FetchMuhlafim still
// exists. Setting the header on the client would send our token to them.
func TestFetchMuhlafim_TokenNotSetOnSharedClient(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	assert.Empty(t, client.Client.Header.Get("Authorization"))
}

func TestFetchMuhlafim_EmptyWindow(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestFetchMuhlafim_Unauthorized(t *testing.T) {
	client := withExternalPayments(t, "tok_wrong", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Contains(t, err.Error(), "401")
}

// Failing before the request makes a Keycloak problem obvious, rather than
// surfacing as a 401 from somewhere else.
func TestFetchMuhlafim_TokenUnavailable(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client := newChargeClient(t)
	client.BaseURL = server.URL
	client.Tokens = stubTokens{err: errors.New("keycloak unreachable")}

	_, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keycloak unreachable")
	assert.False(t, called, "should not reach the server without a token")
}

// A client built without a token source must refuse rather than send an
// unauthenticated request that external_payments would reject anyway.
func TestFetchMuhlafim_NoTokenSource(t *testing.T) {
	client := newChargeClient(t)
	client.BaseURL = "http://127.0.0.1:1"
	client.Tokens = nil

	_, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token source")
}
