package pelecard

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chargeClient(url string) *Client {
	return &Client{Client: resty.New().SetTimeout(5 * time.Second), BaseURL: url, Tokens: staticTokens("tok")}
}

func chargeAt(t *testing.T, handler http.HandlerFunc) (map[string]interface{}, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return chargeClient(srv.URL).ChargeByToken(context.Background(), &ChargeRequest{Reference: "m-7t"},
		Terminal{Name: "token", ChargeURL: srv.URL + "/vh/token/charge"})
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// external_payments answers "pending" for a charge it left in-process: the card
// may have been charged, so it is ErrOutcomeUnknown, not a decline.
func TestChargeByToken_PendingIsOutcomeUnknown(t *testing.T) {
	result, err := chargeAt(t, reply(http.StatusOK, `{"status":"pending","error":"Charge: gateway outcome unknown"}`))

	require.ErrorIs(t, err, ErrOutcomeUnknown)
	assert.Equal(t, "pending", result["status"])
}

// A decline is still an answer, not an error: processOrder reads its status.
func TestChargeByToken_DeclineIsNotAnError(t *testing.T) {
	result, err := chargeAt(t, reply(http.StatusOK, `{"status":"error","error":"Charge error "}`))

	require.NoError(t, err)
	assert.Equal(t, "error", result["status"])
}

// A 5xx can come after the charge (a proxy timing out while external_payments
// waits on Pelecard, or a failure recording a verified charge), so it is
// unknown. A 4xx is refused before any charge and stays a plain error, which
// lets the EMV fallback run.
func TestChargeByToken_ServerErrorIsOutcomeUnknownClientErrorIsNot(t *testing.T) {
	_, err := chargeAt(t, reply(http.StatusBadGateway, "<html>502</html>"))
	require.ErrorIs(t, err, ErrOutcomeUnknown)

	_, err = chargeAt(t, reply(http.StatusBadRequest, `{"status":"error","error":"bad reference"}`))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrOutcomeUnknown)
}

// A 200 that is not JSON says nothing about the charge.
func TestChargeByToken_UnreadableAnswerIsOutcomeUnknown(t *testing.T) {
	_, err := chargeAt(t, reply(http.StatusOK, "<html>ok</html>"))
	require.ErrorIs(t, err, ErrOutcomeUnknown)
}

// The request reached external_payments and the connection dropped without an
// answer: unknown.
func TestChargeByToken_ConnectionDroppedAfterSendIsOutcomeUnknown(t *testing.T) {
	_, err := chargeAt(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		conn, _, hijackErr := w.(http.Hijacker).Hijack()
		if hijackErr != nil {
			t.Errorf("hijack: %v", hijackErr)
			return
		}
		_ = conn.Close()
	})
	require.ErrorIs(t, err, ErrOutcomeUnknown)
}

// Nothing listening: the request never left, so it is a plain error and the
// EMV fallback may run.
func TestChargeByToken_ConnectionRefusedIsNotOutcomeUnknown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := chargeClient(url).ChargeByToken(context.Background(), &ChargeRequest{},
		Terminal{Name: "token", ChargeURL: url + "/vh/token/charge"})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrOutcomeUnknown)
}

// GetChargeStatus asks for the one request: the order's user key in the path,
// the payment's reference in the query.
func TestGetChargeStatus_AsksForTheRequest(t *testing.T) {
	var gotPath, gotReference string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotReference = r.URL.Path, r.URL.Query().Get("reference")
		_, _ = w.Write([]byte(`{"status":"valid"}`))
	}))
	t.Cleanup(srv.Close)

	status, err := chargeClient(srv.URL).GetChargeStatus(context.Background(), "ord-5", "m-7t")

	require.NoError(t, err)
	assert.Equal(t, "valid", status)
	assert.Equal(t, "/vh/charge/ord-5", gotPath)
	assert.Equal(t, "m-7t", gotReference)
}

// No such request at external_payments is ErrPaymentNotFound; any other
// failure is an error, never a status.
func TestGetChargeStatus_NotFoundAndFailures(t *testing.T) {
	srv := httptest.NewServer(reply(http.StatusNotFound, `{"status":"error"}`))
	t.Cleanup(srv.Close)
	_, err := chargeClient(srv.URL).GetChargeStatus(context.Background(), "ord-5", "m-7t")
	require.ErrorIs(t, err, ErrPaymentNotFound)

	srv500 := httptest.NewServer(reply(http.StatusInternalServerError, ""))
	t.Cleanup(srv500.Close)
	status, err := chargeClient(srv500.URL).GetChargeStatus(context.Background(), "ord-5", "m-7t")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrPaymentNotFound))
	assert.Empty(t, status)
}
