package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pelecardmock "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// stubProfileService returns no active HH grant — used in tests that don't exercise HH logic.
type stubProfileService struct{}

func (s *stubProfileService) LookupProfile(_ context.Context, _ string) (*profiles.Profile, error) {
	return nil, nil
}
func (s *stubProfileService) LookupProfileByKeycloakId(_ context.Context, _ string) (*profiles.Profile, error) {
	return nil, nil
}
func (s *stubProfileService) GetProfileByKeycloakID(_ context.Context, _ string) (*profiles.Profile, error) {
	return nil, nil
}
func (s *stubProfileService) GetActiveHHGrant(_ context.Context, _ string) (*profiles.HHGrant, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testRenewalData() *repo.RenewalData {
	return &repo.RenewalData{
		Order: &repo.Order{
			ID:            100,
			Amount:        null.Float64From(80),
			Currency:      null.StringFrom(common.CurrencyNIS),
			OrderLanguage: null.StringFrom("he"),
			AccountID:     null.IntFrom(10),
		},
		Account: &repo.Account{
			ID:        10,
			FirstName: null.StringFrom("Test"),
			LastName:  null.StringFrom("User"),
			Email:     null.StringFrom("test@example.com"),
			Street:    null.StringFrom("Main St"),
			City:      null.StringFrom("TLV"),
			UserKey:   null.StringFrom("kc-123"),
			Country:   null.StringFrom("IL"),
		},
		PrevPayment: &repo.Payment{
			ID:            50,
			AuthNo:        null.StringFrom("AUTH123"),
			PelecardToken: null.StringFrom("TOKEN456"),
		},
		Card: nil,
	}
}

func testRenewalDataWithCard() *repo.RenewalData {
	data := testRenewalData()
	data.Order.CardDetailsId = null.IntFrom(99)
	data.Card = &repo.CardDetails{
		ID:     99,
		Token:  null.StringFrom("CARD_TOKEN_789"),
		Active: null.BoolFrom(true),
		CCNumber:  null.StringFrom("4111****1111"),
		CCExpDate: null.StringFrom("1225"),
	}
	return data
}

func testV1Price() *pricing.ChargePrice {
	return &pricing.ChargePrice{
		Amount:         20,
		Currency:       common.CurrencyUSD,
		PricingVersion: "v1",
	}
}

func testV2Price() *pricing.ChargePrice {
	return &pricing.ChargePrice{
		Amount:         90,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
		V2Evaluation: &pricing.V2PricingEvaluation{
			AccountID:   10,
			CountryCode: "IL",
			FinalPrice:  pricing.Price{Amount: 90, Currency: common.CurrencyNIS},
		},
	}
}

func testPendingPayment() *repo.Payment {
	return &repo.Payment{
		ID:            200,
		OrderID:       null.IntFrom(100),
		Amount:        null.Float64From(90),
		Currency:      null.StringFrom(common.CurrencyNIS),
		PaymentStatus: null.StringFrom(common.PaymentStatusPending),
		ParamX:        null.StringFrom("m-200t"),
		Ordkey:        null.StringFrom("ord-100"),
		AuthNo:        null.StringFrom("AUTH123"),
		PelecardToken: null.StringFrom("TOKEN456"),
	}
}

func gatewaySuccess() map[string]interface{} {
	return map[string]interface{}{"status": "success"}
}

func gatewayDeclined() map[string]interface{} {
	return map[string]interface{}{"status": "declined"}
}

// ---------------------------------------------------------------------------
// BuildChargeRequest
// ---------------------------------------------------------------------------

func TestBuildChargeRequest_UsesResolvedPrice(t *testing.T) {
	data := testRenewalData()
	price := testV2Price()
	payment := testPendingPayment()

	req := BuildChargeRequest(data, price, payment)

	assert.Equal(t, 90.0, req.Price, "should use resolved price, not order amount")
	assert.Equal(t, common.CurrencyNIS, req.Currency, "should use resolved currency")
}

func TestBuildChargeRequest_AccountFields(t *testing.T) {
	data := testRenewalData()
	price := testV1Price()
	payment := testPendingPayment()

	req := BuildChargeRequest(data, price, payment)

	assert.Equal(t, "Test User", req.Name)
	assert.Equal(t, "test@example.com", req.Email)
	assert.Equal(t, "Main St", req.Street)
	assert.Equal(t, "TLV", req.City)
	assert.Equal(t, "he", req.Language)
}

func TestBuildChargeRequest_PaymentFields(t *testing.T) {
	data := testRenewalData()
	price := testV1Price()
	payment := testPendingPayment()

	req := BuildChargeRequest(data, price, payment)

	assert.Equal(t, "ord-100", req.UserKey)
	assert.Equal(t, "m-200t", req.Reference)
	assert.Equal(t, "AUTH123", req.ApprovalNo)
	assert.Equal(t, "TOKEN456", req.Token)
}

func TestBuildChargeRequest_StaticFields(t *testing.T) {
	data := testRenewalData()
	price := testV1Price()
	payment := testPendingPayment()

	req := BuildChargeRequest(data, price, payment)

	assert.Equal(t, "Membership", req.Details)
	assert.Equal(t, "40037", req.SKU)
	assert.Equal(t, "f", req.VAT)
	assert.Equal(t, 1, req.Installments)
	assert.Equal(t, "ben2", req.Organization)
}

func TestBuildChargeRequest_CardOverridesToken(t *testing.T) {
	data := testRenewalDataWithCard()
	price := testV1Price()

	payment := testPendingPayment()
	// CreateRenewalPayment would have set these from the card
	payment.PelecardToken = null.StringFrom("CARD_TOKEN_789")

	req := BuildChargeRequest(data, price, payment)

	assert.Equal(t, "CARD_TOKEN_789", req.Token)
}

// ---------------------------------------------------------------------------
// processOrder
// ---------------------------------------------------------------------------

func TestProcessOrder_CreatePaymentError(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockExecutor := pelecardmock.NewMockChargeExecutor(t)
	emitter := &events.NoopEmitter{}

	data := testRenewalData()
	price := testV1Price()

	mockRepo.EXPECT().CreateRenewalPayment(ctx, data, price.Amount, price.Currency,
		price.PricingVersion, mock.Anything, pelecard.TokenTerminal.PMX).
		Return(nil, errors.New("db error"))

	payment, err := processOrder(ctx, mockRepo, emitter, mockExecutor, data, price, pelecard.TokenTerminal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CreateRenewalPayment")
	assert.Nil(t, payment)
}

func TestProcessOrder_FinalizeError_PostPayment(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockOrdersRepository(t)
	mockExecutor := pelecardmock.NewMockChargeExecutor(t)
	emitter := &events.NoopEmitter{}

	data := testRenewalData()
	price := testV1Price()
	pending := testPendingPayment()

	mockRepo.EXPECT().CreateRenewalPayment(ctx, data, price.Amount, price.Currency,
		price.PricingVersion, mock.Anything, pelecard.TokenTerminal.PMX).Return(pending, nil)
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything, pelecard.TokenTerminal, uint(100)).
		Return(gatewaySuccess(), nil)
	mockRepo.EXPECT().FinalizeRenewal(ctx, uint(100), mock.Anything).
		Return(common.ErrPostPayment)

	payment, err := processOrder(ctx, mockRepo, emitter, mockExecutor, data, price, pelecard.TokenTerminal)
	require.Error(t, err)
	assert.True(t, errors.Is(err, common.ErrPostPayment))
	assert.NotNil(t, payment)
	assert.Equal(t, "1", payment.Success.String, "payment succeeded at gateway even though DB failed")
}

