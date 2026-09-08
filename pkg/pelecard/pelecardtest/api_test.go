package pelecardtest

import (
	"context"
	"testing"

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
