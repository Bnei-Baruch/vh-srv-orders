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

// A genuinely quiet window is not an error, which is also what keeps the
// key-equals-token guard from failing empty months: it errors only when a
// response carried entries and kept none.
func TestFetchMuhlafim_EmptyWindow(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.Empty(t, entries)
}

// The direct Pelecard call skipped entries with no token. Nothing matches "" in
// a caller's token map, so an empty key would be a silent miss rather than an
// error.
func TestFetchMuhlafim_DropsEmptyTokenKey(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"":     {"Token": "", "ActionDescription": "חיוב נקלט"},
			"tok1": {"Token": "tok1", "ActionDescription": "נדחה לא יחויב"}
		}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.NotContains(t, entries, "", "an entry naming no replaced card is dropped")
	assert.Len(t, entries, 1)
	assert.Contains(t, entries, "tok1")
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

// An HTTP 200 whose body is an envelope, not a token map, unmarshals cleanly:
// {"error":{...}} becomes one entry keyed "error" with a zero-value Token. Left
// in, it reports as a window that simply matched no orders.
func TestFetchMuhlafim_DropsEntriesWhoseKeyIsNotTheirToken(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"error": {"code": 502, "message": "upstream"},
			"tok1":  {"Token": "tok1", "ActionDescription": "נדחה לא יחויב"},
			"tok2":  {"Token": "someone-else", "ActionDescription": "חיוב נקלט"}
		}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.NotContains(t, entries, "error", "an envelope entry carries no token")
	assert.NotContains(t, entries, "tok2", "a key that is not its own token matches nothing downstream")
	assert.Equal(t, map[string]pelecard.MuhlafimEntry{
		"tok1": {Token: "tok1", ActionDescription: pelecard.MUH_NIDHA},
	}, entries)
}

// Nothing surviving is the case that bites: returning an empty map with no error
// lets the billing run proceed to charge cards Pelecard reported as replaced,
// and reads exactly like a month with no replacements.
func TestFetchMuhlafim_AllEntriesDropped_IsAnError(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error": {"code": 502, "message": "upstream"}}`))
	})

	entries, err := client.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Contains(t, err.Error(), "none was keyed by its own token")
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
