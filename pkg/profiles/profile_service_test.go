package profiles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keycloakmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
)

func TestProfileServiceAPI_LookupProfile_Success(t *testing.T) {
	expectedProfile := Profile{
		UserID:       uuidPtr(uuid.FromStringOrNil("11000000-0000-0000-0000-000000000000")),
		KeycloakID:   uuidPtr(uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")),
		PrimaryEmail: stringPtr("test@example.com"),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/profiles", r.URL.Path)
		assert.Equal(t, "test@example.com", r.URL.Query().Get("email"))
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))

		response := []Profile{expectedProfile}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfile(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.NotNil(t, profile)
	assert.Equal(t, expectedProfile.PrimaryEmail, profile.PrimaryEmail)
}

func TestProfileServiceAPI_LookupProfile_401RetrySuccess(t *testing.T) {
	expectedProfile := Profile{
		UserID:       uuidPtr(uuid.FromStringOrNil("11000000-0000-0000-0000-000000000000")),
		KeycloakID:   uuidPtr(uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")),
		PrimaryEmail: stringPtr("test@example.com"),
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

		response := []Profile{expectedProfile}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.EXPECT().Invalidate().Once()
	mockTokenSource.EXPECT().Token().Return("token456", nil).Once()

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfile(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.NotNil(t, profile)
	assert.Equal(t, 2, attemptCount)
}

func TestProfileServiceAPI_LookupProfile_401RetryFailure(t *testing.T) {
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

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfile(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, profile)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.Equal(t, 2, attemptCount)
}

func TestProfileServiceAPI_LookupProfile_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfile(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.Nil(t, profile)
}

func TestProfileServiceAPI_LookupProfile_EmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := []Profile{}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfile(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.Nil(t, profile)
}

func TestProfileServiceAPI_LookupProfile_OtherError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: "internal server error"})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	// Invalidate should NOT be called for non-401 errors
	mockTokenSource.AssertNotCalled(t, "Invalidate")

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfile(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, profile)
	assert.Contains(t, err.Error(), "internal server error")
}

func TestProfileServiceAPI_LookupProfileByKeycloakId_Success(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")
	expectedProfile := Profile{
		UserID:       uuidPtr(uuid.FromStringOrNil("11000000-0000-0000-0000-000000000000")),
		KeycloakID:   uuidPtr(keycloakID),
		PrimaryEmail: stringPtr("test@example.com"),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/profile/22000000-0000-0000-0000-000000000000", r.URL.Path)
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedProfile)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfileByKeycloakId(context.Background(), keycloakID.String())

	require.NoError(t, err)
	assert.NotNil(t, profile)
	assert.Equal(t, expectedProfile.KeycloakID, profile.KeycloakID)
}

func TestProfileServiceAPI_LookupProfileByKeycloakId_401RetrySuccess(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")
	expectedProfile := Profile{
		UserID:       uuidPtr(uuid.FromStringOrNil("11000000-0000-0000-0000-000000000000")),
		KeycloakID:   uuidPtr(keycloakID),
		PrimaryEmail: stringPtr("test@example.com"),
	}

	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedProfile)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.EXPECT().Invalidate().Once()
	mockTokenSource.EXPECT().Token().Return("token456", nil).Once()

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfileByKeycloakId(context.Background(), keycloakID.String())

	require.NoError(t, err)
	assert.NotNil(t, profile)
}

func TestProfileServiceAPI_LookupProfileByKeycloakId_NotFound(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.LookupProfileByKeycloakId(context.Background(), keycloakID.String())

	require.NoError(t, err)
	assert.Nil(t, profile)
}

func TestProfileServiceAPI_GetProfileByKeycloakID_Success(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")
	expectedProfile := Profile{
		UserID:       uuidPtr(uuid.FromStringOrNil("11000000-0000-0000-0000-000000000000")),
		KeycloakID:   uuidPtr(keycloakID),
		PrimaryEmail: stringPtr("test@example.com"),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/profile/22000000-0000-0000-0000-000000000000", r.URL.Path)
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedProfile)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.GetProfileByKeycloakID(context.Background(), keycloakID.String())

	require.NoError(t, err)
	assert.NotNil(t, profile)
	assert.Equal(t, expectedProfile.KeycloakID, profile.KeycloakID)
}

func TestProfileServiceAPI_GetProfileByKeycloakID_401RetrySuccess(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")
	expectedProfile := Profile{
		UserID:       uuidPtr(uuid.FromStringOrNil("11000000-0000-0000-0000-000000000000")),
		KeycloakID:   uuidPtr(keycloakID),
		PrimaryEmail: stringPtr("test@example.com"),
	}

	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedProfile)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	mockTokenSource.EXPECT().Invalidate().Once()
	mockTokenSource.EXPECT().Token().Return("token456", nil).Once()

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.GetProfileByKeycloakID(context.Background(), keycloakID.String())

	require.NoError(t, err)
	assert.NotNil(t, profile)
}

func TestProfileServiceAPI_GetProfileByKeycloakID_401RetryFailure(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")

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

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.GetProfileByKeycloakID(context.Background(), keycloakID.String())

	require.Error(t, err)
	assert.Nil(t, profile)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.Equal(t, 2, attemptCount)
}

func TestProfileServiceAPI_GetProfileByKeycloakID_NotFound(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil)

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.GetProfileByKeycloakID(context.Background(), keycloakID.String())

	require.Error(t, err)
	assert.Nil(t, profile)
	assert.Equal(t, ErrNotFound, err)
}

func TestProfileServiceAPI_GetProfileByKeycloakID_OtherError(t *testing.T) {
	keycloakID := uuid.FromStringOrNil("22000000-0000-0000-0000-000000000000")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: "internal server error"})
	}))
	defer server.Close()

	mockTokenSource := keycloakmocks.NewMockTokenSource(t)
	mockTokenSource.EXPECT().Token().Return("token123", nil).Once()
	// Invalidate should NOT be called for non-401 errors
	mockTokenSource.AssertNotCalled(t, "Invalidate")

	api := newTestProfileServiceAPI(server.URL, mockTokenSource)

	profile, err := api.GetProfileByKeycloakID(context.Background(), keycloakID.String())

	require.Error(t, err)
	assert.Nil(t, profile)
	assert.Contains(t, err.Error(), "internal server error")
}

// Helper function to create a test ProfileServiceAPI with a test server URL
func newTestProfileServiceAPI(serverURL string, tokenSource keycloak.TokenSource) *ProfileServiceAPI {
	client := resty.New()
	client.SetBaseURL(serverURL)
	client.SetHeaders(map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   "test",
	})
	client.SetError(APIError{})

	return &ProfileServiceAPI{
		client:      client,
		tokenSource: tokenSource,
	}
}

// Helper functions
func uuidPtr(u uuid.UUID) *uuid.UUID {
	return &u
}

func stringPtr(s string) *string {
	return &s
}
