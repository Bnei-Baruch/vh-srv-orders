package accounting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keycloakmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg/keycloak"
)

func TestAccountingServiceAPI_GetEuropeContributions_Success(t *testing.T) {
	expected := &EuropeContributionsResult{
		CutoffDate:     "2025-06-10",
		LookbackMonths: 12,
		Results: []EuropeContributionEntry{
			{IdentifierType: "email", Identifier: "user@example.com", Found: true, Contributions: map[string]float64{"EUR": 1758}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/europe/contributions/batch", r.URL.Path)
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"emails":["user@example.com"]}`, string(body), "lookback_months must be omitted")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data:    expected,
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestAccountingServiceAPI_GetEuropeContributions_MixedFoundNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data: &EuropeContributionsResult{
				LookbackMonths: 12,
				Results: []EuropeContributionEntry{
					{IdentifierType: "email", Identifier: "donor@example.com", Found: true, Contributions: map[string]float64{"EUR": 500}},
					{IdentifierType: "email", Identifier: "missing@example.com", Found: false, Contributions: map[string]float64{}},
				},
			},
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"donor@example.com", "missing@example.com"})

	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	assert.True(t, result.Results[0].Found)
	assert.False(t, result.Results[1].Found)
}

func TestAccountingServiceAPI_GetEuropeContributions_NegativeAmounts(t *testing.T) {
	// Refunds are subtracted upstream — totals may be negative and must round-trip.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data: &EuropeContributionsResult{
				Results: []EuropeContributionEntry{
					{IdentifierType: "email", Identifier: "refunded@example.com", Found: true, Contributions: map[string]float64{"EUR": -12.5}},
				},
			},
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"refunded@example.com"})

	require.NoError(t, err)
	assert.Equal(t, -12.5, result.Results[0].Contributions["EUR"])
}

func TestAccountingServiceAPI_GetEuropeContributions_EmptyEmailsSkipsHTTPCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for an empty email list")
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Results)
}

func TestAccountingServiceAPI_GetEuropeContributions_NullData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"Fetched!","data":null,"success":true}`))
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "malformed response")
}

func TestAccountingServiceAPI_GetEuropeContributions_401RetrySuccess(t *testing.T) {
	expected := &EuropeContributionsResult{
		LookbackMonths: 12,
		Results: []EuropeContributionEntry{
			{IdentifierType: "email", Identifier: "user@example.com", Found: true, Contributions: map[string]float64{"EUR": 75}},
		},
	}

	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(APIError{Error: "unauthorized"})
			return
		}

		assert.Equal(t, "Bearer token456", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data:    expected,
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.EXPECT().Invalidate().Once()
	mockTokenSource.EXPECT().Token().Return("token456", nil).Once()

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	assert.Equal(t, 2, attemptCount)
}

func TestAccountingServiceAPI_GetEuropeContributions_OtherError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: "internal server error"})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.AssertNotCalled(t, "Invalidate")

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "internal server error")
}

func TestAccountingServiceAPI_GetEuropeContributions_TokenSourceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when token source fails")
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("", assert.AnError)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestAccountingServiceAPI_GetEuropeContributions_CacheHit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data: &EuropeContributionsResult{
				Results: []EuropeContributionEntry{
					{IdentifierType: "email", Identifier: "user@example.com", Found: true, Contributions: map[string]float64{"EUR": 100}},
				},
			},
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)
	api.SetCacheEnabled(true)

	first, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})
	require.NoError(t, err)
	second, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, callCount, "second call should be served from cache")
}

func TestAccountingServiceAPI_GetEuropeContributions_CacheKeyOrderInsensitive(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data:    &EuropeContributionsResult{},
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)
	api.SetCacheEnabled(true)

	_, err := api.GetEuropeContributions(context.Background(), []string{"a@example.com", "B@example.com"})
	require.NoError(t, err)
	_, err = api.GetEuropeContributions(context.Background(), []string{"b@example.com", "A@example.com"})
	require.NoError(t, err)

	assert.Equal(t, 1, callCount, "same email set in different order/casing should hit the cache")
}

func TestAccountingServiceAPI_GetEuropeContributions_CacheDisabledByDefault(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(europeContributionsResponse{
			Message: "Fetched!",
			Data:    &EuropeContributionsResult{},
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	_, err := api.GetEuropeContributions(context.Background(), []string{"user@example.com"})
	require.NoError(t, err)
	_, err = api.GetEuropeContributions(context.Background(), []string{"user@example.com"})
	require.NoError(t, err)

	assert.Equal(t, 2, callCount, "no caching unless explicitly enabled")
}
