package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
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

func TestClearAllFlags(t *testing.T) {
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

	// Create orders with different flags
	order1 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(200.0),
		Flag:      null.StringFrom(common.OrderFlagSkip),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)

	order3 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(300.0),
		Flag:      null.StringFrom("other_flag"),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)

	// Verify flags are set
	var flag1, flag2, flag3 null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order1.ID).Scan(&flag1)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, flag1.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order2.ID).Scan(&flag2)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagSkip, flag2.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order3.ID).Scan(&flag3)
	require.NoError(t, err)
	assert.Equal(t, "other_flag", flag3.String)

	// Clear all flags
	err = db.ClearAllFlags(ctx)
	require.NoError(t, err)

	// Verify all flags are cleared
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order1.ID).Scan(&flag1)
	require.NoError(t, err)
	assert.Equal(t, "", flag1.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order2.ID).Scan(&flag2)
	require.NoError(t, err)
	assert.Equal(t, "", flag2.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order3.ID).Scan(&flag3)
	require.NoError(t, err)
	assert.Equal(t, "", flag3.String)
}

func TestUpdateOrdersUserKeyFromAccounts(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create accounts with UserKey
	account1 := Account{
		Email:   null.StringFrom("user1@example.com"),
		UserKey: null.StringFrom("userkey1"),
	}
	accountID1, err := db.CreateAccount(ctx, account1)
	require.NoError(t, err)

	account2 := Account{
		Email:   null.StringFrom("user2@example.com"),
		UserKey: null.StringFrom("userkey2"),
	}
	accountID2, err := db.CreateAccount(ctx, account2)
	require.NoError(t, err)

	// Create orders with different userkey values (or null)
	order1 := Order{
		AccountID: null.IntFrom(accountID1),
		Amount:    null.Float64From(100.0),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)

	// Set order1's userkey to something different
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, "old_userkey1", order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID: null.IntFrom(accountID2),
		Amount:    null.Float64From(200.0),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)

	// Verify initial state
	var userkey1, userkey2 null.String
	err = db.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, order1.ID).Scan(&userkey1)
	require.NoError(t, err)
	assert.Equal(t, "old_userkey1", userkey1.String)

	err = db.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, order2.ID).Scan(&userkey2)
	require.NoError(t, err)
	// order2 might have null or empty userkey initially

	// Update orders userkey from accounts
	err = db.UpdateOrdersUserKeyFromAccounts(ctx)
	require.NoError(t, err)

	// Verify userkeys were updated
	err = db.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, order1.ID).Scan(&userkey1)
	require.NoError(t, err)
	assert.Equal(t, "userkey1", userkey1.String, "Order1 userkey should be updated from account1")

	err = db.QueryRow(ctx, `SELECT userkey FROM orders WHERE id = $1`, order2.ID).Scan(&userkey2)
	require.NoError(t, err)
	assert.Equal(t, "userkey2", userkey2.String, "Order2 userkey should be updated from account2")
}

func TestGetPaidOrdersCount(t *testing.T) {
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

	// Set up test month: January 2024
	year := 2024
	month := 1
	lastDay := time.Date(year, time.Month(month), 31, 23, 59, 59, 0, time.UTC)

	// Create paid orders within the month
	order1 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 15, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(200.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 20, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)

	// Create order outside the month (should not be counted)
	order3 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(300.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month-1), 28, 12, 0, 0, 0, time.UTC)), // Previous month
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)

	// Create order with different status (should not be counted)
	order4 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(400.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusCancelled),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 25, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order4)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order4.ID)
	require.NoError(t, err)

	// Create order with different product type (should not be counted)
	order5 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(500.0),
		ProductType: null.StringFrom("tickets"),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 18, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order5)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order5.ID)
	require.NoError(t, err)

	// Get count
	count, err := db.GetPaidOrdersCount(ctx, year, month, lastDay)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "Should count only paid globalmembership orders in the specified month")
}

func TestGetOrdersToSkipDouble(t *testing.T) {
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

	// Set up test month: January 2024
	year := 2024
	month := 1
	lastDay := time.Date(year, time.Month(month), 31, 23, 59, 59, 0, time.UTC)

	// User with multiple paid orders (should be included)
	userkey1 := "userkey1"
	order1 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 15, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey1, order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(200.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 20, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey1, order2.ID)
	require.NoError(t, err)

	// User with one paid and one cancelled order (should be included - 2 total)
	userkey2 := "userkey2"
	order3 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(300.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 10, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey2, order3.ID)
	require.NoError(t, err)

	order4 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(400.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusCancelled),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 12, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order4)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order4.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey2, order4.ID)
	require.NoError(t, err)

	// User with only one paid order (should NOT be included)
	userkey3 := "userkey3"
	order5 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(500.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 18, 12, 0, 0, 0, time.UTC)),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order5)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order5.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey3, order5.ID)
	require.NoError(t, err)

	// User with orders outside the month (should NOT be included)
	userkey4 := "userkey4"
	order6 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(600.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month-1), 28, 12, 0, 0, 0, time.UTC)), // Previous month
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order6)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order6.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey4, order6.ID)
	require.NoError(t, err)

	order7 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(700.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month-1), 29, 12, 0, 0, 0, time.UTC)), // Previous month
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order7)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order7.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey4, order7.ID)
	require.NoError(t, err)

	// Get userkeys to skip
	userkeys, err := db.GetOrdersToSkipDouble(ctx, year, month, lastDay)
	require.NoError(t, err)

	// Should return userkey1 and userkey2 (both have >1 paid/cancelled orders in the month)
	assert.Len(t, userkeys, 2)
	assert.Contains(t, userkeys, userkey1, "userkey1 should be included (2 paid orders)")
	assert.Contains(t, userkeys, userkey2, "userkey2 should be included (1 paid + 1 cancelled)")
	assert.NotContains(t, userkeys, userkey3, "userkey3 should NOT be included (only 1 order)")
	assert.NotContains(t, userkeys, userkey4, "userkey4 should NOT be included (orders outside month)")
}

func TestGetOrdersToSkipFresh(t *testing.T) {
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

	// Set up test month: January 2024
	year := 2024
	month := 1
	lastDay := time.Date(year, time.Month(month), 31, 23, 59, 59, 0, time.UTC)

	// User with paid order and empty flag (should be included)
	userkey1 := "userkey1"
	order1 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(100.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 15, 12, 0, 0, 0, time.UTC)),
		Flag:        null.StringFrom(""), // Empty flag
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey1, order1.ID)
	require.NoError(t, err)

	// User with cancelled order and empty flag (should be included)
	userkey2 := "userkey2"
	order2 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(200.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusCancelled),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 20, 12, 0, 0, 0, time.UTC)),
		Flag:        null.StringFrom(""), // Empty flag
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey2, order2.ID)
	require.NoError(t, err)

	// User with paid order but non-empty flag (should NOT be included)
	userkey3 := "userkey3"
	order3 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(300.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 18, 12, 0, 0, 0, time.UTC)),
		Flag:        null.StringFrom(common.OrderFlagSkip), // Non-empty flag
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey3, order3.ID)
	require.NoError(t, err)

	// User with order outside the month (should NOT be included)
	userkey4 := "userkey4"
	order4 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(400.0),
		ProductType: null.StringFrom(common.ProductTypeGlobalMembership),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month-1), 28, 12, 0, 0, 0, time.UTC)), // Previous month
		Flag:        null.StringFrom(""),                                                            // Empty flag
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order4)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order4.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey4, order4.ID)
	require.NoError(t, err)

	// User with different product type (should NOT be included)
	userkey5 := "userkey5"
	order5 := Order{
		AccountID:   null.IntFrom(accountID),
		Amount:      null.Float64From(500.0),
		ProductType: null.StringFrom("tickets"),
		Status:      null.StringFrom(common.OrderStatusPaid),
		PaymentDate: null.TimeFrom(time.Date(year, time.Month(month), 22, 12, 0, 0, 0, time.UTC)),
		Flag:        null.StringFrom(""), // Empty flag
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order5)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order5.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey5, order5.ID)
	require.NoError(t, err)

	// Get userkeys to skip
	userkeys, err := db.GetOrdersToSkipFresh(ctx, year, month, lastDay)
	require.NoError(t, err)

	// Should return userkey1 and userkey2 (both have paid/cancelled orders with empty flag in the month)
	assert.Len(t, userkeys, 2)
	assert.Contains(t, userkeys, userkey1, "userkey1 should be included (paid order with empty flag)")
	assert.Contains(t, userkeys, userkey2, "userkey2 should be included (cancelled order with empty flag)")
	assert.NotContains(t, userkeys, userkey3, "userkey3 should NOT be included (non-empty flag)")
	assert.NotContains(t, userkeys, userkey4, "userkey4 should NOT be included (order outside month)")
	assert.NotContains(t, userkeys, userkey5, "userkey5 should NOT be included (different product type)")
}

func TestSkipOrdersByUserKey(t *testing.T) {
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

	userkey := "test_userkey"

	// Create orders with "torenew" flag (should be updated)
	order1 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order1)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order1.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey, order1.ID)
	require.NoError(t, err)

	order2 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(200.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order2)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order2.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey, order2.ID)
	require.NoError(t, err)

	// Create order with "torenew" flag but different userkey (should NOT be updated)
	order3 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(300.0),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order3)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order3.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, "different_userkey", order3.ID)
	require.NoError(t, err)

	// Create order with same userkey but different flag (should NOT be updated)
	order4 := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(400.0),
		Flag:      null.StringFrom(common.OrderFlagSkip),
	}
	createString, numString, createQueryArgs = prepareOrderCreateQuery(order4)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order4.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE orders SET userkey = $1 WHERE id = $2`, userkey, order4.ID)
	require.NoError(t, err)

	// Verify initial state
	var flag1, flag2, flag3, flag4 null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order1.ID).Scan(&flag1)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, flag1.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order2.ID).Scan(&flag2)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, flag2.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order3.ID).Scan(&flag3)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, flag3.String)

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order4.ID).Scan(&flag4)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagSkip, flag4.String)

	// Skip orders by userkey
	rowsAffected, err := db.SkipOrdersByUserKey(ctx, userkey)
	require.NoError(t, err)
	assert.Equal(t, 2, rowsAffected, "Should update 2 orders")

	// Verify orders were updated correctly
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order1.ID).Scan(&flag1)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagSkip, flag1.String, "Order1 should be updated to skip")

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order2.ID).Scan(&flag2)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagSkip, flag2.String, "Order2 should be updated to skip")

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order3.ID).Scan(&flag3)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, flag3.String, "Order3 should NOT be updated (different userkey)")

	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order4.ID).Scan(&flag4)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagSkip, flag4.String, "Order4 should NOT be updated (different flag)")

	// Test with userkey that has no matching orders
	rowsAffected, err = db.SkipOrdersByUserKey(ctx, "nonexistent_userkey")
	require.NoError(t, err)
	assert.Equal(t, 0, rowsAffected, "Should update 0 orders for nonexistent userkey")
}

// mockChargeExecutor implements ChargeExecutor for testing
type mockChargeExecutor struct {
	response map[string]interface{}
	err      error
}

func (m *mockChargeExecutor) Execute(ctx context.Context, request *RequestPayment, pmx string, orderID uint) (map[string]interface{}, error) {
	return m.response, m.err
}

func TestTryRenewalWithTerminal_Success(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create card details with token
	cardDetails := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("1234****5678"),
		CCExpDate:       null.StringFrom("12/25"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("test_token_123"),
	}
	cardDetailsID, err := db.CreateCardDetailsAndGetId(ctx, cardDetails)
	require.NoError(t, err)

	// Create order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Link card to order
	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID, order.ID)
	require.NoError(t, err)

	// Create initial payment
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom("test_token_123"),
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Insert into payments_pelecard (required for update later)
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, payment.ID)
	require.NoError(t, err)

	// Set up mock charge executor that returns success
	mockExecutor := &mockChargeExecutor{
		response: map[string]interface{}{
			"status": "success",
		},
		err: nil,
	}
	db.SetDryRunChargeExecutor(mockExecutor)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify no error
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify payment was updated correctly
	assert.Equal(t, "success", result.PaymentStatus.String)
	assert.Equal(t, "1", result.Success.String)
	assert.Equal(t, "test_term", result.Terminal.String)

	// Verify order flag was updated to "renewed"
	var orderFlag null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order.ID).Scan(&orderFlag)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagRenewed, orderFlag.String)

	// Verify order status was updated to "paid"
	var orderStatus null.String
	err = db.QueryRow(ctx, `SELECT "Status" FROM orders WHERE id = $1`, order.ID).Scan(&orderStatus)
	require.NoError(t, err)
	assert.Equal(t, common.OrderStatusPaid, orderStatus.String)
}

func TestTryRenewalWithTerminal_PaymentDeclined(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create card details with token
	cardDetails := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("1234****5678"),
		CCExpDate:       null.StringFrom("12/25"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("test_token_123"),
	}
	cardDetailsID, err := db.CreateCardDetailsAndGetId(ctx, cardDetails)
	require.NoError(t, err)

	// Create order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Link card to order
	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID, order.ID)
	require.NoError(t, err)

	// Create initial payment
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom("test_token_123"),
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Insert into payments_pelecard
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, payment.ID)
	require.NoError(t, err)

	// Set up mock charge executor that returns declined
	mockExecutor := &mockChargeExecutor{
		response: map[string]interface{}{
			"status": "declined",
		},
		err: nil,
	}
	db.SetDryRunChargeExecutor(mockExecutor)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify no error (payment declined is not an error, just unsuccessful)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify payment was updated correctly
	assert.Equal(t, "failed", result.PaymentStatus.String)
	assert.Equal(t, "0", result.Success.String)
	assert.Equal(t, "test_term", result.Terminal.String)

	// Verify order flag was NOT updated to "renewed" (payment failed)
	var orderFlag null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order.ID).Scan(&orderFlag)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, orderFlag.String, "Flag should remain torenew on failed payment")

	// Verify order status was updated to "nosuccess"
	var orderStatus null.String
	err = db.QueryRow(ctx, `SELECT "Status" FROM orders WHERE id = $1`, order.ID).Scan(&orderStatus)
	require.NoError(t, err)
	assert.Equal(t, common.OrderStatusNoSuccess, orderStatus.String)
}

func TestTryRenewalWithTerminal_PaymentGatewayError(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create card details with token
	cardDetails := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("1234****5678"),
		CCExpDate:       null.StringFrom("12/25"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("test_token_123"),
	}
	cardDetailsID, err := db.CreateCardDetailsAndGetId(ctx, cardDetails)
	require.NoError(t, err)

	// Create order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Link card to order
	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID, order.ID)
	require.NoError(t, err)

	// Create initial payment
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom("test_token_123"),
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Insert into payments_pelecard
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, payment.ID)
	require.NoError(t, err)

	// Set up mock charge executor that returns an error
	mockExecutor := &mockChargeExecutor{
		response: nil,
		err:      fmt.Errorf("gateway connection error"),
	}
	db.SetDryRunChargeExecutor(mockExecutor)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payment failed")
	assert.NotNil(t, result)

	// Verify payment was updated to failed
	assert.Equal(t, "failed", result.PaymentStatus.String)
	assert.Equal(t, "0", result.Success.String)
	assert.Equal(t, "test_term", result.Terminal.String)

	// Verify order flag was NOT updated to "renewed"
	var orderFlag null.String
	err = db.QueryRow(ctx, `SELECT "Flag" FROM orders WHERE id = $1`, order.ID).Scan(&orderFlag)
	require.NoError(t, err)
	assert.Equal(t, common.OrderFlagToRenew, orderFlag.String)

	// Verify order status was updated to "nosuccess"
	var orderStatus null.String
	err = db.QueryRow(ctx, `SELECT "Status" FROM orders WHERE id = $1`, order.ID).Scan(&orderStatus)
	require.NoError(t, err)
	assert.Equal(t, common.OrderStatusNoSuccess, orderStatus.String)
}

func TestTryRenewalWithTerminal_OrderNotFound(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Execute TryRenewalWithTerminal with non-existent order
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, 99999, terminal)

	// Verify error is wrapped with ErrPrePayment
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
	assert.Contains(t, err.Error(), "o.GetOrderByID")
	assert.Nil(t, result)
}

func TestTryRenewalWithTerminal_PaymentNotFound(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create order without payment
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify error is wrapped with ErrPrePayment
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
	assert.Contains(t, err.Error(), "o.GetPaymentForOrderID")
	assert.Nil(t, result)
}

func TestTryRenewalWithTerminal_AccountNotFound(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create order without account (this will fail foreign key constraint, so we use a different approach)
	// We'll create an order with a valid account, then delete the account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Create payment
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom("test_token_123"),
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Delete the account (this might fail due to foreign key constraints, so we'll just test with a non-linked account)
	// Actually, let's update order to point to non-existent account
	_, err = db.Exec(ctx, `UPDATE orders SET "AccountID" = 99999 WHERE id = $1`, order.ID)
	require.NoError(t, err)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify error is wrapped with ErrPrePayment
	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrPrePayment)
	assert.Contains(t, err.Error(), "o.GetAccountForOrderID")
	assert.Nil(t, result)
}

func TestTryRenewalWithTerminal_NoTokenAvailable(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create order without card details
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Create payment without token
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom(""), // Empty token
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Insert into payments_pelecard
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, payment.ID)
	require.NoError(t, err)

	// Set up mock charge executor that simulates token validation failure
	mockExecutor := &mockChargeExecutor{
		response: map[string]interface{}{
			"status": "failed",
		},
		err: nil,
	}
	db.SetDryRunChargeExecutor(mockExecutor)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Empty token doesn't prevent payment attempt - it just fails at gateway
	// So we expect no error (DB updates succeed) but payment marked as failed
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "failed", result.PaymentStatus.String)
	assert.Equal(t, "0", result.Success.String)
}

func TestTryRenewalWithTerminal_TerminalNameIsStored(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create card details with token
	cardDetails := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("1234****5678"),
		CCExpDate:       null.StringFrom("12/25"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("test_token_123"),
	}
	cardDetailsID, err := db.CreateCardDetailsAndGetId(ctx, cardDetails)
	require.NoError(t, err)

	// Create order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Link card to order
	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID, order.ID)
	require.NoError(t, err)

	// Create initial payment
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom("test_token_123"),
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Insert into payments_pelecard
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, payment.ID)
	require.NoError(t, err)

	// Set up mock charge executor
	mockExecutor := &mockChargeExecutor{
		response: map[string]interface{}{
			"status": "success",
		},
		err: nil,
	}
	db.SetDryRunChargeExecutor(mockExecutor)

	// Execute TryRenewalWithTerminal with specific terminal name
	terminal := pelecard.Terminal{Name: "mycustom", PMX: "custom_pmx"}
	result, err := db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify no error
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify terminal name is stored in the returned payment
	assert.Equal(t, "mycustom", result.Terminal.String)

	// Verify terminal name is persisted in the database (payments_pelecard table)
	// Note: use result.ID because TryRenewalWithTerminal creates a new payment record
	var storedTerminal null.String
	err = db.QueryRow(ctx, `SELECT terminal FROM payments_pelecard WHERE payment_id = $1`, result.ID).Scan(&storedTerminal)
	require.NoError(t, err)
	assert.Equal(t, "mycustom", storedTerminal.String)
}

func TestTryRenewalWithTerminal_EventEmitted(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)

	// Create a test event emitter to capture events
	emitter := new(events.NoopEmitter)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, emitter)
	require.NoError(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	// Create account
	accountID, err := db.CreateAccount(ctx, Account{
		Email: null.StringFrom("test@example.com"),
	})
	require.NoError(t, err)

	// Create card details with token
	cardDetails := CardDetails{
		AccountID:       null.IntFrom(accountID),
		GatewayProvider: null.StringFrom("pelecard"),
		CCNumber:        null.StringFrom("1234****5678"),
		CCExpDate:       null.StringFrom("12/25"),
		Active:          null.BoolFrom(true),
		Token:           null.StringFrom("test_token_123"),
	}
	cardDetailsID, err := db.CreateCardDetailsAndGetId(ctx, cardDetails)
	require.NoError(t, err)

	// Create order
	order := Order{
		AccountID: null.IntFrom(accountID),
		Amount:    null.Float64From(100.0),
		Type:      null.StringFrom("recurring"),
		Status:    null.StringFrom(common.OrderStatusPaid),
		Flag:      null.StringFrom(common.OrderFlagToRenew),
	}
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&order.ID)
	require.NoError(t, err)

	// Link card to order
	_, err = db.Exec(ctx, `UPDATE orders SET card_details_id = $1 WHERE id = $2`, cardDetailsID, order.ID)
	require.NoError(t, err)

	// Create initial payment
	payment := Payment{
		OrderID:       null.IntFrom(order.ID),
		PelecardToken: null.StringFrom("test_token_123"),
		PaymentStatus: null.StringFrom("success"),
		Success:       null.StringFrom("1"),
	}
	createPString, numPString, createPArgs := preparePaymentCreateQuery(payment)
	err = db.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createPString, numPString),
		createPArgs...).Scan(&payment.ID)
	require.NoError(t, err)

	// Insert into payments_pelecard
	_, err = db.Exec(ctx, `INSERT INTO payments_pelecard (payment_id) VALUES ($1)`, payment.ID)
	require.NoError(t, err)

	// Set up mock charge executor
	mockExecutor := &mockChargeExecutor{
		response: map[string]interface{}{
			"status": "success",
		},
		err: nil,
	}
	db.SetDryRunChargeExecutor(mockExecutor)

	// Execute TryRenewalWithTerminal
	terminal := pelecard.Terminal{Name: "test_term", PMX: "test_pmx"}
	_, err = db.TryRenewalWithTerminal(ctx, uint(order.ID), terminal)

	// Verify no error (event emission is tested by the fact that the function completes without panic)
	require.NoError(t, err)
}

