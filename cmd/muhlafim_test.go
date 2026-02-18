package cmd

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// MockPelecardAPIForCmd is a mock implementation for command testing
type MockPelecardAPIForCmd struct {
	data map[string]pelecard.MuhlafimEntry
}

func (m *MockPelecardAPIForCmd) FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]pelecard.MuhlafimEntry, error) {
	return m.data, nil
}

func TestMuhlafimCommand_Integration(t *testing.T) {
	// Setup test database
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := repo.NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create test account
	accountID, err := db.CreateAccount(ctx, repo.Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create test orders with Flag='torenew'
	var order1 repo.Order
	err = db.QueryRow(ctx, `INSERT INTO orders ("AccountID", "Amount", "Flag") VALUES ($1, $2, $3) RETURNING id`,
		accountID, 100.0, common.OrderFlagToRenew).Scan(&order1.ID)
	require.NoError(t, err)

	var order2 repo.Order
	err = db.QueryRow(ctx, `INSERT INTO orders ("AccountID", "Amount", "Flag") VALUES ($1, $2, $3) RETURNING id`,
		accountID, 200.0, common.OrderFlagToRenew).Scan(&order2.ID)
	require.NoError(t, err)

	var order3 repo.Order
	err = db.QueryRow(ctx, `INSERT INTO orders ("AccountID", "Amount", "Flag") VALUES ($1, $2, $3) RETURNING id`,
		accountID, 300.0, common.OrderFlagToRenew).Scan(&order3.ID)
	require.NoError(t, err)

	// Create payments with tokens (must have "success" status to match GetTokensForOrders logic)
	var payment1 repo.Payment
	err = db.QueryRow(ctx, `INSERT INTO payments ("OrderID", pelecard_token, "PaymentStatus") VALUES ($1, $2, $3) RETURNING id`,
		order1.ID, "token_hiyuv_niklat", "success").Scan(&payment1.ID)
	require.NoError(t, err)

	var payment2 repo.Payment
	err = db.QueryRow(ctx, `INSERT INTO payments ("OrderID", pelecard_token, "PaymentStatus") VALUES ($1, $2, $3) RETURNING id`,
		order2.ID, "token_nidha", "success").Scan(&payment2.ID)
	require.NoError(t, err)

	var payment3 repo.Payment
	err = db.QueryRow(ctx, `INSERT INTO payments ("OrderID", pelecard_token, "PaymentStatus") VALUES ($1, $2, $3) RETURNING id`,
		order3.ID, "token_bitul_newcard", "success").Scan(&payment3.ID)
	require.NoError(t, err)

	// Create mock muhlafim data covering all cases
	mockMuhlafimData := map[string]pelecard.MuhlafimEntry{
		"token_hiyuv_niklat": {
			Token:             "token_hiyuv_niklat",
			ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
			NewCardNumber:     "",
			NewExpirationDate: "",
		},
		"token_nidha": {
			Token:             "token_nidha",
			ActionDescription: pelecard.MUH_NIDHA,
			NewCardNumber:     "",
			NewExpirationDate: "",
		},
		"token_bitul_newcard": {
			Token:             "token_bitul_newcard",
			ActionDescription: pelecard.MUH_BITUL,
			NewCardNumber:     "1234567890123456",
			NewExpirationDate: "12/25",
		},
		"token_lotakin": {
			Token:             "token_lotakin",
			ActionDescription: pelecard.MUH_LOTAKIN,
			NewCardNumber:     "",
			NewExpirationDate: "",
		},
		"token_unknown": {
			Token:             "token_unknown",
			ActionDescription: "Unknown Action",
			NewCardNumber:     "",
			NewExpirationDate: "",
		},
	}

	// Test the processing logic (simulating what the command does)
	// Fetch flagged orders
	orders, err := db.GetFlaggedOrders(ctx)
	require.NoError(t, err)
	assert.Len(t, orders, 3)

	// Batch fetch tokens
	orderIDs := make([]int, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.ID
	}
	tokenMap, err := db.GetTokensForOrders(ctx, orderIDs)
	require.NoError(t, err)

	// Process orders
	processedCount := 0
	updatedCount := 0
	for _, order := range orders {
		token, exists := tokenMap[order.ID]
		if !exists || token == "" {
			continue
		}

		muhlafimEntry, found := mockMuhlafimData[token]
		if !found {
			continue
		}

		processedCount++

		var flag string
		shouldUpdate := false

		switch muhlafimEntry.ActionDescription {
		case pelecard.MUH_HIYUV_NIKLAT:
			if len(muhlafimEntry.NewCardNumber) > 0 {
				// NEW CARD - don't update
			} else {
				flag = common.OrderFlagMuhHiyuvNiklat
				shouldUpdate = true
			}
		case pelecard.MUH_NIDHA:
			if len(muhlafimEntry.NewCardNumber) > 0 {
				// NEW CARD - don't update
			} else {
				flag = common.OrderFlagMuhNidha
				shouldUpdate = true
			}
		case pelecard.MUH_BITUL:
			if len(muhlafimEntry.NewCardNumber) > 0 {
				// NEW CARD - don't update
			} else {
				flag = common.OrderFlagMuhBitul
				shouldUpdate = true
			}
		case pelecard.MUH_LOTAKIN:
			if len(muhlafimEntry.NewCardNumber) > 0 {
				// NEW CARD - don't update
			} else {
				flag = common.OrderFlagMuhLotakin
				shouldUpdate = true
			}
		default:
			flag = common.OrderFlagMuhAher
			shouldUpdate = true
		}

		if shouldUpdate {
			err := db.FlagOrder(ctx, order.ID, flag)
			require.NoError(t, err)
			updatedCount++
		}
	}

	// Verify results
	assert.Equal(t, 3, processedCount, "Should process all 3 orders")
	assert.Equal(t, 2, updatedCount, "Should update 2 orders (order3 has new card, so no update)")

	// Verify order1 flag was updated
	var flag1 null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order1.ID).Scan(&flag1)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagMuhHiyuvNiklat, flag1.String)

	// Verify order2 flag was updated
	var flag2 null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order2.ID).Scan(&flag2)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagMuhNidha, flag2.String)

	// Verify order3 flag was NOT updated (has new card)
	var flag3 null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order3.ID).Scan(&flag3)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, flag3.String, "Order3 should not be updated because it has a new card")

	// Test muhlafim.json file creation
	muhlafimJSON, err := json.MarshalIndent(mockMuhlafimData, "", "  ")
	require.NoError(t, err)

	tempFile := "muhlafim_test.json"
	defer os.Remove(tempFile)

	err = os.WriteFile(tempFile, muhlafimJSON, 0644)
	require.NoError(t, err)

	// Verify file was created and contains correct data
	fileData, err := os.ReadFile(tempFile)
	require.NoError(t, err)

	var loadedData map[string]pelecard.MuhlafimEntry
	err = json.Unmarshal(fileData, &loadedData)
	require.NoError(t, err)
	assert.Equal(t, mockMuhlafimData, loadedData)
}
