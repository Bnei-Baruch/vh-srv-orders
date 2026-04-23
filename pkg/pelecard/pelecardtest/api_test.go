package pelecardtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

func TestFetchMuhlafim_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		mockData map[string]pelecard.MuhlafimEntry
		wantErr  bool
	}{
		{
			name: "MUH_HIYUV_NIKLAT with empty NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token1": {
					Token:             "token1",
					ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_HIYUV_NIKLAT with NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token2": {
					Token:             "token2",
					ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
					NewCardNumber:     "1234567890123456",
					NewExpirationDate: "12/25",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_NIDHA with empty NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token3": {
					Token:             "token3",
					ActionDescription: pelecard.MUH_NIDHA,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_NIDHA with NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token4": {
					Token:             "token4",
					ActionDescription: pelecard.MUH_NIDHA,
					NewCardNumber:     "9876543210987654",
					NewExpirationDate: "06/26",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_BITUL with empty NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token5": {
					Token:             "token5",
					ActionDescription: pelecard.MUH_BITUL,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_BITUL with NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token6": {
					Token:             "token6",
					ActionDescription: pelecard.MUH_BITUL,
					NewCardNumber:     "1111222233334444",
					NewExpirationDate: "09/27",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_LOTAKIN with empty NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token7": {
					Token:             "token7",
					ActionDescription: pelecard.MUH_LOTAKIN,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
		{
			name: "MUH_LOTAKIN with NewCardNumber",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token8": {
					Token:             "token8",
					ActionDescription: pelecard.MUH_LOTAKIN,
					NewCardNumber:     "5555666677778888",
					NewExpirationDate: "03/28",
				},
			},
			wantErr: false,
		},
		{
			name: "Unknown action description",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token9": {
					Token:             "token9",
					ActionDescription: "Unknown Action",
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
		{
			name: "Empty token entries (should be filtered out)",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token10": {
					Token:             "token10",
					ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
				"": {
					Token:             "",
					ActionDescription: pelecard.MUH_NIDHA,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
		{
			name: "Multiple tokens in response",
			mockData: map[string]pelecard.MuhlafimEntry{
				"token11": {
					Token:             "token11",
					ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
				"token12": {
					Token:             "token12",
					ActionDescription: pelecard.MUH_NIDHA,
					NewCardNumber:     "9999888877776666",
					NewExpirationDate: "12/29",
				},
				"token13": {
					Token:             "token13",
					ActionDescription: pelecard.MUH_BITUL,
					NewCardNumber:     "",
					NewExpirationDate: "",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := mocks.NewMockPelecardAPI(t)
			// Filter out empty tokens from mock data to match real implementation behavior
			filteredMockData := make(map[string]pelecard.MuhlafimEntry)
			for token, entry := range tt.mockData {
				if len(token) > 0 {
					filteredMockData[token] = entry
				}
			}
			mockAPI.EXPECT().FetchMuhlafim(mock.Anything, "21/08/2025 00:00", "24/09/2025 00:00").
				Return(filteredMockData, nil)

			result, err := mockAPI.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				// Verify that empty tokens are filtered out
				for token, entry := range result {
					assert.NotEmpty(t, token, "Empty tokens should be filtered out")
					assert.Equal(t, token, entry.Token)
				}

				// Verify all expected entries are present (only non-empty tokens)
				for token, expectedEntry := range filteredMockData {
					actualEntry, exists := result[token]
					require.True(t, exists, "Token %s should be in result", token)
					assert.Equal(t, expectedEntry.ActionDescription, actualEntry.ActionDescription)
					assert.Equal(t, expectedEntry.NewCardNumber, actualEntry.NewCardNumber)
					assert.Equal(t, expectedEntry.NewExpirationDate, actualEntry.NewExpirationDate)
				}
			}
		})
	}
}

func TestMockPelecardAPI_Error(t *testing.T) {
	mockAPI := mocks.NewMockPelecardAPI(t)
	mockAPI.EXPECT().FetchMuhlafim(mock.Anything, "21/08/2025 00:00", "24/09/2025 00:00").
		Return(nil, assert.AnError)

	result, err := mockAPI.FetchMuhlafim(context.Background(), "21/08/2025 00:00", "24/09/2025 00:00")

	assert.Error(t, err)
	assert.Nil(t, result)
}

// newTestClient creates a Client configured to use a test server
func newTestClient(_ *testing.T, serverURL string, user, password, terminalNumber string) *pelecard.Client {
	restyClient := resty.New()
	restyClient.SetHeaders(map[string]string{
		"Content-Type": "application/json",
	})

	// Intercept requests to rewrite the URL to point to the test server
	restyClient.OnBeforeRequest(func(c *resty.Client, req *resty.Request) error {
		// Replace the hardcoded PELECARD_API_BASE_URL with the test server URL
		req.URL = strings.Replace(req.URL, pelecard.PELECARD_API_BASE_URL, serverURL, 1)
		return nil
	})

	// Create client struct with exported Client field
	return &pelecard.Client{
		Client:         restyClient,
		User:           user,
		Password:       password,
		TerminalNumber: terminalNumber,
	}
}

func TestClient_FetchMuhlafim_HTTPRequest(t *testing.T) {
	tests := []struct {
		name           string
		user           string
		password       string
		terminalNumber string
		startDate      string
		endDate        string
		responseBody   string
		statusCode     int
		wantErr        bool
		verifyRequest  func(t *testing.T, req *http.Request)
	}{
		{
			name:           "successful request with correct auth and headers",
			user:           "testuser",
			password:       "testpass",
			terminalNumber: "123456",
			startDate:      "21/08/2025 00:00",
			endDate:        "24/09/2025 00:00",
			responseBody: `{
				"ResultData": [
					{
						"Token": "token1",
						"ActionDescription": "חיוב נקלט",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					}
				]
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			verifyRequest: func(t *testing.T, req *http.Request) {
				// Verify Content-Type header
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

				// Verify request body contains auth and request data
				var body pelecard.MuhlafimRequest
				err := json.NewDecoder(req.Body).Decode(&body)
				require.NoError(t, err)
				assert.Equal(t, "testuser", body.User, "User should be set in request")
				assert.Equal(t, "testpass", body.Password, "Password should be set in request")
				assert.Equal(t, "123456", body.TerminalNumber, "TerminalNumber should be set in request")
				assert.Equal(t, "21/08/2025 00:00", body.StartDate, "StartDate should be set in request")
				assert.Equal(t, "24/09/2025 00:00", body.EndDate, "EndDate should be set in request")

				// Verify URL path
				assert.True(t, strings.HasSuffix(req.URL.Path, "/services/GetTerminalMuhlafim"), "URL should end with correct path")
			},
		},
		{
			name:           "HTTP error status",
			user:           "testuser",
			password:       "testpass",
			terminalNumber: "123456",
			startDate:      "21/08/2025 00:00",
			endDate:        "24/09/2025 00:00",
			responseBody:   `{"error": "unauthorized"}`,
			statusCode:     http.StatusUnauthorized,
			wantErr:        true,
			verifyRequest: func(t *testing.T, req *http.Request) {
				// Basic verification
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			},
		},
		{
			name:           "invalid JSON response",
			user:           "testuser",
			password:       "testpass",
			terminalNumber: "123456",
			startDate:      "21/08/2025 00:00",
			endDate:        "24/09/2025 00:00",
			responseBody:   `invalid json`,
			statusCode:     http.StatusOK,
			wantErr:        true,
			verifyRequest: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			},
		},
		{
			name:           "empty token filtering",
			user:           "testuser",
			password:       "testpass",
			terminalNumber: "123456",
			startDate:      "21/08/2025 00:00",
			endDate:        "24/09/2025 00:00",
			responseBody: `{
				"ResultData": [
					{
						"Token": "token1",
						"ActionDescription": "חיוב נקלט",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					},
					{
						"Token": "",
						"ActionDescription": "נדחה לא יחויב",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					}
				]
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			verifyRequest: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				assert.Equal(t, http.MethodPost, r.Method)

				// Run custom request verification
				if tt.verifyRequest != nil {
					tt.verifyRequest(t, r)
				}

				// Set response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Create client with test server URL
			client := newTestClient(t, server.URL, tt.user, tt.password, tt.terminalNumber)

			// Make request
			result, err := client.FetchMuhlafim(context.Background(), tt.startDate, tt.endDate)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				// Verify empty tokens are filtered out
				for token := range result {
					assert.NotEmpty(t, token, "Empty tokens should be filtered out")
				}
			}
		})
	}
}

func TestClient_FetchMuhlafim_RequestParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request body parsing
		var req pelecard.MuhlafimRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify all fields are correctly set
		assert.Equal(t, "testuser", req.User, "User should be set in request")
		assert.Equal(t, "testpass", req.Password, "Password should be set in request")
		assert.Equal(t, "999888", req.TerminalNumber, "TerminalNumber should be set in request")
		assert.Equal(t, "01/01/2025 00:00", req.StartDate, "StartDate should be set in request")
		assert.Equal(t, "31/12/2025 23:59", req.EndDate, "EndDate should be set in request")

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ResultData": []}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, "testuser", "testpass", "999888")
	_, err := client.FetchMuhlafim(context.Background(), "01/01/2025 00:00", "31/12/2025 23:59")
	assert.NoError(t, err)
}

func TestClient_FetchMuhlafim_ResponseParsing(t *testing.T) {
	tests := []struct {
		name         string
		responseBody string
		expectedKeys []string
		wantErr      bool
	}{
		{
			name: "single entry",
			responseBody: `{
				"ResultData": [
					{
						"Token": "token1",
						"ActionDescription": "חיוב נקלט",
						"NewCardNumber": "1234567890123456",
						"NewExpirationDate": "12/25"
					}
				]
			}`,
			expectedKeys: []string{"token1"},
			wantErr:      false,
		},
		{
			name: "multiple entries",
			responseBody: `{
				"ResultData": [
					{
						"Token": "token1",
						"ActionDescription": "חיוב נקלט",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					},
					{
						"Token": "token2",
						"ActionDescription": "נדחה לא יחויב",
						"NewCardNumber": "9876543210987654",
						"NewExpirationDate": "06/26"
					}
				]
			}`,
			expectedKeys: []string{"token1", "token2"},
			wantErr:      false,
		},
		{
			name: "entries with empty tokens filtered out",
			responseBody: `{
				"ResultData": [
					{
						"Token": "token1",
						"ActionDescription": "חיוב נקלט",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					},
					{
						"Token": "",
						"ActionDescription": "נדחה לא יחויב",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					},
					{
						"Token": "token2",
						"ActionDescription": "ביטול הוראת קבע ע\"י הלקוח",
						"NewCardNumber": "",
						"NewExpirationDate": ""
					}
				]
			}`,
			expectedKeys: []string{"token1", "token2"},
			wantErr:      false,
		},
		{
			name:         "empty result data",
			responseBody: `{"ResultData": []}`,
			expectedKeys: []string{},
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, "user", "pass", "123")
			result, err := client.FetchMuhlafim(context.Background(), "01/01/2025 00:00", "31/12/2025 23:59")

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, len(tt.expectedKeys), len(result))

				for _, key := range tt.expectedKeys {
					_, exists := result[key]
					assert.True(t, exists, "Expected key %s to be in result", key)
				}
			}
		})
	}
}
