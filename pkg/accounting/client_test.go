package accounting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keycloakmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
)

func TestAccountingServiceAPI_GetLastContributions_Success(t *testing.T) {
	expected := &ContributionsResult{
		Found: true,
		Total: map[string]float64{"USD": 1200},
		Companies: []CompanyContributions{
			{
				CompanyID:     "9130353260751136",
				CompanyName:   "KabU",
				Found:         true,
				Contributions: map[string]float64{"USD": 1200},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/quickbooks/contributions", r.URL.Path)
		assert.Equal(t, "user@example.com", r.URL.Query().Get("email"))
		assert.False(t, r.URL.Query().Has("company_id"))
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(contributionsResponse{
			Message: "Fetched!",
			Data:    expected,
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetLastContributions(context.Background(), "user@example.com", nil)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestAccountingServiceAPI_GetLastContributions_WithCompanyID(t *testing.T) {
	expected := &ContributionsResult{
		Found: true,
		Total: map[string]float64{"USD": 50},
		Companies: []CompanyContributions{
			{
				CompanyID:     "9341454118",
				CompanyName:   "ARI",
				Found:         true,
				Contributions: map[string]float64{"USD": 50},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/quickbooks/contributions", r.URL.Path)
		assert.Equal(t, "user@example.com", r.URL.Query().Get("email"))
		assert.Equal(t, "9341454118", r.URL.Query().Get("company_id"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(contributionsResponse{
			Message: "Fetched!",
			Data:    expected,
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	companyID := "9341454118"
	result, err := api.GetLastContributions(context.Background(), "user@example.com", &companyID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestAccountingServiceAPI_GetLastContributions_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(contributionsResponse{
			Message: "Fetched!",
			Data: &ContributionsResult{
				Found:     false,
				Total:     map[string]float64{},
				Companies: []CompanyContributions{},
			},
			Success: true,
		})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetLastContributions(context.Background(), "missing@example.com", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Found)
	assert.Empty(t, result.Total)
	assert.Empty(t, result.Companies)
}

func TestAccountingServiceAPI_GetLastContributions_NullData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"Fetched!","data":null,"success":true}`))
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetLastContributions(context.Background(), "user@example.com", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "malformed response")
}

func TestAccountingServiceAPI_GetLastContributions_401RetrySuccess(t *testing.T) {
	expected := &ContributionsResult{
		Found:     true,
		Total:     map[string]float64{"USD": 75},
		Companies: []CompanyContributions{{CompanyID: "C1", CompanyName: "Co", Found: true, Contributions: map[string]float64{"USD": 75}}},
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
		json.NewEncoder(w).Encode(contributionsResponse{
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

	result, err := api.GetLastContributions(context.Background(), "user@example.com", nil)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	assert.Equal(t, 2, attemptCount)
}

func TestAccountingServiceAPI_GetLastContributions_401RetryFailure(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(APIError{Error: "unauthorized"})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.EXPECT().Invalidate().Once()
	mockTokenSource.EXPECT().Token().Return("token456", nil).Once()

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetLastContributions(context.Background(), "user@example.com", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.Equal(t, 2, attemptCount)
}

func TestAccountingServiceAPI_GetLastContributions_OtherError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: "internal server error"})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.AssertNotCalled(t, "Invalidate")

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetLastContributions(context.Background(), "user@example.com", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "internal server error")
}

func TestAccountingServiceAPI_GetLastContributions_TokenSourceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when token source fails")
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("", assert.AnError)

	api := newTestAccountingServiceAPI(server.URL, mockTokenSource)

	result, err := api.GetLastContributions(context.Background(), "user@example.com", nil)

	require.Error(t, err)
	assert.Nil(t, result)
}

func newTestAccountingServiceAPI(serverURL string, tokenSource keycloak.TokenSource) *AccountingServiceAPI {
	client := resty.New()
	client.SetBaseURL(serverURL)
	client.SetHeaders(map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   "test",
	})
	client.SetError(APIError{})

	return &AccountingServiceAPI{
		client:      client,
		tokenSource: tokenSource,
	}
}
