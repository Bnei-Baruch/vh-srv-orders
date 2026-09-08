package pelecardtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// rotatingTokens hands out the next token after each Invalidate, so a retry can
// be told apart from the attempt that preceded it.
type rotatingTokens struct {
	tokens      []string
	idx         int
	invalidated int
}

func (r *rotatingTokens) Token() (string, error) { return r.tokens[r.idx], nil }
func (r *rotatingTokens) Invalidate() {
	r.invalidated++
	if r.idx < len(r.tokens)-1 {
		r.idx++
	}
}

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
	var authHeader, method, path string
	var query url.Values

	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		method = r.Method
		path = r.URL.Path
		query = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tok1": {"Token":"tok1","ActionDescription":"חיוב נקלט","NewCardNumber":"1234","NewExpirationDate":"0130"},
			"tok2": {"Token":"tok2","ActionDescription":"נדחה לא יחויב"}
		}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	assert.Equal(t, "Bearer tok_secret", authHeader)
	assert.Equal(t, http.MethodGet, method, "a read, not a post")
	assert.Equal(t, "/token/muhlafim", path)
	assert.Equal(t, "21/08/2025 00:00", query.Get("StartDate"))
	assert.Equal(t, "24/09/2025 00:00", query.Get("EndDate"))

	require.Len(t, entries, 2)
	assert.Equal(t, "1234", entries["tok1"].NewCardNumber)
	assert.Equal(t, "0130", entries["tok1"].NewExpirationDate)
	assert.Equal(t, "נדחה לא יחויב", entries["tok2"].ActionDescription)
}

// The request carries no terminal and no credentials — that is the whole point
// of external_payments owning the call.
func TestFetchMuhlafim_SendsNoCredentials(t *testing.T) {
	var query url.Values

	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	for _, forbidden := range []string{"user", "password", "terminalNumber", "TerminalNumber"} {
		assert.NotContains(t, query, forbidden)
	}
}

// The resty client is shared with ChargeByToken, which sets no Authorization
// header of its own. Setting ours on the client rather than the request would
// attach it to every call the client makes.
func TestFetchMuhlafim_TokenNotSetOnSharedClient(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	assert.Empty(t, client.Client.Header.Get("Authorization"))
}

// A quiet window is a normal answer.
func TestFetchMuhlafim_EmptyWindow(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A token cached a moment before expiry is sent and rejected. Without the retry
// the error reaches BillingService.processMuhlafim and aborts the monthly run.
func TestFetchMuhlafim_RetriesOnceAfter401(t *testing.T) {
	var seen []string
	client := withExternalPayments(t, "unused", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if len(seen) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Write([]byte(`{"tok1": {"Token": "tok1", "ActionDescription": "חיוב נקלט"}}`))
	})
	tokens := &rotatingTokens{tokens: []string{"stale", "fresh"}}
	client.Tokens = tokens

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, 1, tokens.invalidated, "the stale token is invalidated once")
	require.Len(t, seen, 2, "exactly one retry")
	assert.Equal(t, []string{"Bearer stale", "Bearer fresh"}, seen)
}

func TestFetchMuhlafim_RetriesOnlyOnce(t *testing.T) {
	var requests int
	client := withExternalPayments(t, "unused", func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	})
	client.Tokens = &rotatingTokens{tokens: []string{"stale", "fresh"}}

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Contains(t, err.Error(), "401")
	assert.Equal(t, 2, requests, "a persistent 401 fails rather than looping")
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
