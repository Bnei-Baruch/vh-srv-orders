package pelecardtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// newChargeClient creates a Client for ChargeByToken/Execute tests.
// No URL rewriting is needed because terminal.ChargeURL is set directly to the test server URL.
func newChargeClient(t *testing.T) *pelecard.Client {
	t.Helper()
	restyClient := resty.New()
	restyClient.SetHeaders(map[string]string{
		"Content-Type": "application/json",
	})
	return &pelecard.Client{
		Client:         restyClient,
		User:           "user",
		Password:       "pass",
		TerminalNumber: "123",
	}
}

// --- TerminalByPMX ---

func TestTerminalByPMX_Token(t *testing.T) {
	terminal := pelecard.TerminalByPMX("t")

	assert.Equal(t, pelecard.TokenTerminal, terminal)
	assert.Equal(t, "token", terminal.Name)
	assert.Equal(t, "t", terminal.PMX)
	assert.NotEmpty(t, terminal.ChargeURL)
}

func TestTerminalByPMX_EMV(t *testing.T) {
	terminal := pelecard.TerminalByPMX("e")

	assert.Equal(t, pelecard.EMVTerminal, terminal)
	assert.Equal(t, "emv", terminal.Name)
	assert.Equal(t, "e", terminal.PMX)
	assert.NotEmpty(t, terminal.ChargeURL)
}

func TestTerminalByPMX_Unknown(t *testing.T) {
	terminal := pelecard.TerminalByPMX("x")

	assert.Equal(t, "x", terminal.PMX)
	assert.Empty(t, terminal.Name)
	assert.Empty(t, terminal.ChargeURL)
}

func TestTerminalByPMX_Empty(t *testing.T) {
	terminal := pelecard.TerminalByPMX("")

	assert.Empty(t, terminal.PMX)
	assert.Empty(t, terminal.Name)
	assert.Empty(t, terminal.ChargeURL)
}

// --- ChargeByToken ---

func TestClient_ChargeByToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"PelecardTransactionId": "TX123", "StatusCode": "000"}`))
	}))
	defer server.Close()

	client := newChargeClient(t)
	terminal := pelecard.TokenTerminal
	terminal.ChargeURL = server.URL

	result, err := client.ChargeByToken(context.Background(), &pelecard.ChargeRequest{
		Token: "tok_abc",
		Price: 99.90,
	}, terminal)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "TX123", result["PelecardTransactionId"])
	assert.Equal(t, "000", result["StatusCode"])
}

func TestClient_ChargeByToken_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	client := newChargeClient(t)
	terminal := pelecard.TokenTerminal
	terminal.ChargeURL = server.URL

	result, err := client.ChargeByToken(context.Background(), &pelecard.ChargeRequest{Token: "tok"}, terminal)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "401")
}

func TestClient_ChargeByToken_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := newChargeClient(t)
	terminal := pelecard.TokenTerminal
	terminal.ChargeURL = server.URL

	result, err := client.ChargeByToken(context.Background(), &pelecard.ChargeRequest{Token: "tok"}, terminal)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestClient_ChargeByToken_EmptyChargeURL(t *testing.T) {
	client := newChargeClient(t)
	terminal := pelecard.Terminal{Name: "unknown", PMX: "x"} // ChargeURL is empty

	result, err := client.ChargeByToken(context.Background(), &pelecard.ChargeRequest{Token: "tok"}, terminal)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no charge URL")
}

func TestClient_ChargeByToken_RequestBody(t *testing.T) {
	var captured pelecard.ChargeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"StatusCode": "000"}`))
	}))
	defer server.Close()

	client := newChargeClient(t)
	terminal := pelecard.TokenTerminal
	terminal.ChargeURL = server.URL

	req := &pelecard.ChargeRequest{
		Token:        "tok_xyz",
		Price:        149.50,
		Currency:     "ILS",
		Name:         "Test User",
		Email:        "test@example.com",
		Installments: 1,
		Reference:    "ref-001",
	}
	_, err := client.ChargeByToken(context.Background(), req, terminal)
	require.NoError(t, err)

	assert.Equal(t, "tok_xyz", captured.Token)
	assert.Equal(t, 149.50, captured.Price)
	assert.Equal(t, "ILS", captured.Currency)
	assert.Equal(t, "Test User", captured.Name)
	assert.Equal(t, "test@example.com", captured.Email)
	assert.Equal(t, 1, captured.Installments)
	assert.Equal(t, "ref-001", captured.Reference)
}

// --- Execute (live client) ---

func TestClient_Execute_DelegatesToChargeByToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"StatusCode": "000"}`))
	}))
	defer server.Close()

	client := newChargeClient(t)
	terminal := pelecard.TokenTerminal
	terminal.ChargeURL = server.URL

	result, err := client.Execute(context.Background(), &pelecard.ChargeRequest{Token: "tok"}, terminal, 42)

	require.NoError(t, err)
	assert.Equal(t, "000", result["StatusCode"])
}

func TestClient_Execute_OrderIDIsIgnored(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"StatusCode": "000"}`))
	}))
	defer server.Close()

	client := newChargeClient(t)
	terminal := pelecard.TokenTerminal
	terminal.ChargeURL = server.URL
	req := &pelecard.ChargeRequest{Token: "tok"}

	_, err := client.Execute(context.Background(), req, terminal, 1)
	require.NoError(t, err)
	_, err = client.Execute(context.Background(), req, terminal, 99999)
	require.NoError(t, err)

	assert.Equal(t, 2, requestCount, "each Execute call should make exactly one HTTP request regardless of orderID")
}

func TestClient_Execute_PropagatesChargeByTokenError(t *testing.T) {
	client := newChargeClient(t)
	terminal := pelecard.Terminal{Name: "bad", PMX: "x"} // empty ChargeURL

	result, err := client.Execute(context.Background(), &pelecard.ChargeRequest{}, terminal, 1)

	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- DryRunChargeExecutor ---
//
// Hash function: h = uint64(orderID) * 2654435761 % 100
//   orderID=0 → h=0  → h < 15         → both terminals fail
//   orderID=2 → h=22 → 15 <= h < 45   → token fails, EMV succeeds
//   orderID=1 → h=61 → h >= 45        → token succeeds, EMV fails

func TestDryRunChargeExecutor_BothTerminalsFail(t *testing.T) {
	executor := pelecard.NewDryRunChargeExecutor()
	req := &pelecard.ChargeRequest{Token: "tok"}

	tokenResult, err := executor.Execute(context.Background(), req, pelecard.TokenTerminal, 0)
	require.NoError(t, err)
	assert.Equal(t, "declined", tokenResult["status"])

	emvResult, err := executor.Execute(context.Background(), req, pelecard.EMVTerminal, 0)
	require.NoError(t, err)
	assert.Equal(t, "declined", emvResult["status"])
}

func TestDryRunChargeExecutor_TokenFailsEMVSucceeds(t *testing.T) {
	executor := pelecard.NewDryRunChargeExecutor()
	req := &pelecard.ChargeRequest{Token: "tok"}

	tokenResult, err := executor.Execute(context.Background(), req, pelecard.TokenTerminal, 2)
	require.NoError(t, err)
	assert.Equal(t, "declined", tokenResult["status"])

	emvResult, err := executor.Execute(context.Background(), req, pelecard.EMVTerminal, 2)
	require.NoError(t, err)
	assert.Equal(t, "success", emvResult["status"])
}

func TestDryRunChargeExecutor_TokenSucceedsEMVFails(t *testing.T) {
	executor := pelecard.NewDryRunChargeExecutor()
	req := &pelecard.ChargeRequest{Token: "tok"}

	tokenResult, err := executor.Execute(context.Background(), req, pelecard.TokenTerminal, 1)
	require.NoError(t, err)
	assert.Equal(t, "success", tokenResult["status"])

	emvResult, err := executor.Execute(context.Background(), req, pelecard.EMVTerminal, 1)
	require.NoError(t, err)
	assert.Equal(t, "declined", emvResult["status"])
}

func TestDryRunChargeExecutor_IsDeterministic(t *testing.T) {
	executor := pelecard.NewDryRunChargeExecutor()
	req := &pelecard.ChargeRequest{Token: "tok"}

	for _, orderID := range []uint{0, 1, 2, 42, 100, 999} {
		first, err := executor.Execute(context.Background(), req, pelecard.TokenTerminal, orderID)
		require.NoError(t, err)
		second, err := executor.Execute(context.Background(), req, pelecard.TokenTerminal, orderID)
		require.NoError(t, err)
		assert.Equal(t, first["status"], second["status"], "orderID %d should always produce the same result", orderID)
	}
}

func TestDryRunChargeExecutor_RequestIsIgnored(t *testing.T) {
	executor := pelecard.NewDryRunChargeExecutor()

	// orderID=1 → h=61 → token succeeds regardless of request content
	nilResult, err := executor.Execute(context.Background(), nil, pelecard.TokenTerminal, 1)
	require.NoError(t, err)
	assert.Equal(t, "success", nilResult["status"])

	emptyResult, err := executor.Execute(context.Background(), &pelecard.ChargeRequest{}, pelecard.TokenTerminal, 1)
	require.NoError(t, err)
	assert.Equal(t, "success", emptyResult["status"])
}

func TestDryRunChargeExecutor_Distribution(t *testing.T) {
	// Since GCD(2654435761, 100) = 1, orderIDs 0–99 produce all 100 distinct values 0–99.
	// Bucket counts are therefore exact: 15 / 30 / 55.
	executor := pelecard.NewDryRunChargeExecutor()
	req := &pelecard.ChargeRequest{}

	var failBoth, failToken, succeedToken int
	for i := uint(0); i < 100; i++ {
		tokenRes, err := executor.Execute(context.Background(), req, pelecard.TokenTerminal, i)
		require.NoError(t, err)
		emvRes, err := executor.Execute(context.Background(), req, pelecard.EMVTerminal, i)
		require.NoError(t, err)

		switch {
		case tokenRes["status"] == "declined" && emvRes["status"] == "declined":
			failBoth++
		case tokenRes["status"] == "declined" && emvRes["status"] == "success":
			failToken++
		default:
			succeedToken++
		}
	}

	assert.Equal(t, 15, failBoth, "15%% of orders should fail both terminals")
	assert.Equal(t, 30, failToken, "30%% of orders should fail token but succeed on EMV")
	assert.Equal(t, 55, succeedToken, "55%% of orders should succeed on token")
}
