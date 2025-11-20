package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

func TestGetTokensForOrders(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create an account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create orders
	order1 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(200.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)

	order3 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(300.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)

	// Create payments with tokens (must have "success" status to match GetPaymentForOrderID logic)
	payment1 := Payment{
		OrderID:       null.IntFrom(order1.ID),
		PelecardToken: null.StringFrom("token1"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment1.ID)
	require.NoError(t, err)

	// Create a second payment for order1 with a different token (should get the first one by id ASC)
	payment1b := Payment{
		OrderID:       null.IntFrom(order1.ID),
		PelecardToken: null.StringFrom("token1_latest"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(payment1b)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment1b.ID)
	require.NoError(t, err)

	payment2 := Payment{
		OrderID:       null.IntFrom(order2.ID),
		PelecardToken: null.StringFrom("token2"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(payment2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment2.ID)
	require.NoError(t, err)

	// order3 has no payment

	// Test batch fetching tokens
	orderIDs := []int{order1.ID, order2.ID, order3.ID}
	tokenMap, err := db.GetTokensForOrders(ctx, orderIDs)
	require.NoError(t, err)

	// Verify results
	assert.Len(t, tokenMap, 3)
	assert.Equal(t, "token1", tokenMap[order1.ID], "Should get the first token for order1 (ASC ordering)")
	assert.Equal(t, "token2", tokenMap[order2.ID], "Should get token for order2")
	assert.Equal(t, "", tokenMap[order3.ID], "Should return empty string for order3 with no payment")

	// Test with empty slice
	emptyMap, err := db.GetTokensForOrders(ctx, []int{})
	require.NoError(t, err)
	assert.Empty(t, emptyMap)
}

func TestGetTokensForOrdersWithCardDetailsFallback(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create an account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Test Case 1: Order with CardDetailsId and active card with token (should use card token)
	orderWithCard := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(orderWithCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderWithCard.ID)
	require.NoError(t, err)

	// Create CardDetails with active card and token
	cardDetails1 := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("1234****5678"),
		CCExpDate:       null.StringFrom("12/25"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("card_token_1"),
	}
	cardDetailsID1, err := db.CreateCardDetailsAndGetId(ctx, cardDetails1)
	require.NoError(t, err)

	// Link card to order
	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID1, orderWithCard.ID)
	require.NoError(t, err)

	// Create a payment for this order (should be ignored in favor of card token)
	paymentWithCard := Payment{
		OrderID:       null.IntFrom(orderWithCard.ID),
		PelecardToken: null.StringFrom("payment_token_1"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(paymentWithCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&paymentWithCard.ID)
	require.NoError(t, err)

	// Test Case 2: Order with CardDetailsId but inactive card (should fallback to payment)
	orderWithInactiveCard := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(200.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(orderWithInactiveCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderWithInactiveCard.ID)
	require.NoError(t, err)

	cardDetails2 := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("2345****6789"),
		CCExpDate:       null.StringFrom("12/26"),
		Active:          null.BoolFrom(false), // inactive
		Token:           null.StringFrom("card_token_2"),
	}
	cardDetailsID2, err := db.CreateCardDetailsAndGetId(ctx, cardDetails2)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID2, orderWithInactiveCard.ID)
	require.NoError(t, err)

	paymentInactiveCard := Payment{
		OrderID:       null.IntFrom(orderWithInactiveCard.ID),
		PelecardToken: null.StringFrom("payment_token_2"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(paymentInactiveCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&paymentInactiveCard.ID)
	require.NoError(t, err)

	// Test Case 3: Order with CardDetailsId but no token in card (should fallback to payment)
	orderWithNoTokenCard := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(300.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(orderWithNoTokenCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderWithNoTokenCard.ID)
	require.NoError(t, err)

	cardDetails3 := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("3456****7890"),
		CCExpDate:       null.StringFrom("12/27"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom(""), // empty token
	}
	cardDetailsID3, err := db.CreateCardDetailsAndGetId(ctx, cardDetails3)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID3, orderWithNoTokenCard.ID)
	require.NoError(t, err)

	paymentNoTokenCard := Payment{
		OrderID:       null.IntFrom(orderWithNoTokenCard.ID),
		PelecardToken: null.StringFrom("payment_token_3"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(paymentNoTokenCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&paymentNoTokenCard.ID)
	require.NoError(t, err)

	// Test Case 4: Order with CardDetailsId but deleted card (should fallback to payment)
	orderWithDeletedCard := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(400.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(orderWithDeletedCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderWithDeletedCard.ID)
	require.NoError(t, err)

	cardDetails4 := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("4567****8901"),
		CCExpDate:       null.StringFrom("12/28"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("card_token_4"),
	}
	cardDetailsID4, err := db.CreateCardDetailsAndGetId(ctx, cardDetails4)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID4, orderWithDeletedCard.ID)
	require.NoError(t, err)

	// Soft delete the card
	err = db.SoftDeleteCardDetailById(ctx, cardDetailsID4)
	require.NoError(t, err)

	paymentDeletedCard := Payment{
		OrderID:       null.IntFrom(orderWithDeletedCard.ID),
		PelecardToken: null.StringFrom("payment_token_4"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(paymentDeletedCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&paymentDeletedCard.ID)
	require.NoError(t, err)

	// Test Case 5: Order without CardDetailsId (should use payment token)
	orderWithoutCard := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(500.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(orderWithoutCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderWithoutCard.ID)
	require.NoError(t, err)

	paymentNoCard := Payment{
		OrderID:       null.IntFrom(orderWithoutCard.ID),
		PelecardToken: null.StringFrom("payment_token_5"),
		PaymentStatus: null.StringFrom("success"),
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(paymentNoCard)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&paymentNoCard.ID)
	require.NoError(t, err)

	// Test Case 6: Order with no CardDetailsId and no payment (should return empty string)
	orderNoCardNoPayment := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(600.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(orderNoCardNoPayment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderNoCardNoPayment.ID)
	require.NoError(t, err)

	// Test Case 7: Order with only non-success payments (should return empty string, not use failed payment)
	orderWithFailedPayment := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(700.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(orderWithFailedPayment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&orderWithFailedPayment.ID)
	require.NoError(t, err)

	paymentFailed := Payment{
		OrderID:       null.IntFrom(orderWithFailedPayment.ID),
		PelecardToken: null.StringFrom("failed_payment_token"),
		PaymentStatus: null.StringFrom("failed"), // non-success status
	}
	createPString, numPString, createPArgs = preparePaymentCreateQuery(paymentFailed)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&paymentFailed.ID)
	require.NoError(t, err)

	// Test batch fetching tokens with all scenarios
	orderIDs := []int{
		orderWithCard.ID,
		orderWithInactiveCard.ID,
		orderWithNoTokenCard.ID,
		orderWithDeletedCard.ID,
		orderWithoutCard.ID,
		orderNoCardNoPayment.ID,
		orderWithFailedPayment.ID,
	}
	tokenMap, err := db.GetTokensForOrders(ctx, orderIDs)
	require.NoError(t, err)

	// Verify results
	assert.Len(t, tokenMap, 7)
	assert.Equal(t, "card_token_1", tokenMap[orderWithCard.ID], "Should use card token when card is active and has token")
	assert.Equal(t, "payment_token_2", tokenMap[orderWithInactiveCard.ID], "Should fallback to payment when card is inactive")
	assert.Equal(t, "payment_token_3", tokenMap[orderWithNoTokenCard.ID], "Should fallback to payment when card has no token")
	assert.Equal(t, "payment_token_4", tokenMap[orderWithDeletedCard.ID], "Should fallback to payment when card is deleted")
	assert.Equal(t, "payment_token_5", tokenMap[orderWithoutCard.ID], "Should use payment token when no CardDetailsId")
	assert.Equal(t, "", tokenMap[orderNoCardNoPayment.ID], "Should return empty string when no card and no payment")
	assert.Equal(t, "", tokenMap[orderWithFailedPayment.ID], "Should ignore non-success payments and return empty string")
}
