package pelecardtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// withExternalPayments starts a stub external_payments, sets the token, and
// returns a client pointed at the stub. Everything is restored afterwards.
func withExternalPayments(t *testing.T, token string, handler http.HandlerFunc) *pelecard.Client {
	t.Helper()
	server := httptest.NewServer(handler)

	originalToken := common.Config.ExternalPaymentsToken
	common.Config.ExternalPaymentsToken = token

	t.Cleanup(func() {
		common.Config.ExternalPaymentsToken = originalToken
		server.Close()
	})

	client := newChargeClient(t)
	client.BaseURL = server.URL
	return client
}

func TestFetchMuhlafimExternal_SendsTokenAndParsesEntries(t *testing.T) {
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

	entries, err := client.FetchMuhlafimExternal(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
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
func TestFetchMuhlafimExternal_SendsNoCredentials(t *testing.T) {
	var raw map[string]any

	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafimExternal(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	for _, forbidden := range []string{"user", "password", "terminalNumber", "TerminalNumber"} {
		assert.NotContains(t, raw, forbidden)
	}
}

// The shared client also posts directly to Pelecard while FetchMuhlafim still
// exists. Setting the header on the client would send our token to them.
func TestFetchMuhlafimExternal_TokenNotSetOnSharedClient(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafimExternal(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")
	require.NoError(t, err)

	assert.Empty(t, client.Client.Header.Get("Authorization"))
}

func TestFetchMuhlafimExternal_EmptyWindow(t *testing.T) {
	client := withExternalPayments(t, "tok_secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})

	entries, err := client.FetchMuhlafimExternal(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestFetchMuhlafimExternal_Unauthorized(t *testing.T) {
	client := withExternalPayments(t, "tok_wrong", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	})

	entries, err := client.FetchMuhlafimExternal(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Contains(t, err.Error(), "401")
}

// Failing before the request makes a missing token obvious, rather than
// surfacing as a 401 from somewhere else.
func TestFetchMuhlafimExternal_NoTokenConfigured(t *testing.T) {
	var called bool
	client := withExternalPayments(t, "", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	})

	_, err := client.FetchMuhlafimExternal(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "EXTERNAL_PAYMENTS_TOKEN")
	assert.False(t, called, "should not reach the server without a token")
}
