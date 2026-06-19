package pricing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// hhGrantProviderFunc is a function adapter that implements repo.HHGrantProvider.
type hhGrantProviderFunc func(ctx context.Context, keycloakID string) (*repo.HHGrant, error)

func (f hhGrantProviderFunc) GetActiveHHGrant(ctx context.Context, keycloakID string) (*repo.HHGrant, error) {
	return f(ctx, keycloakID)
}

func hhGrant(id, pct int) *repo.HHGrant {
	return &repo.HHGrant{
		ID:          id,
		KeycloakID:  "kc-1",
		Type:        common.HHGrantTypeOther,
		DiscountPct: pct,
		EndDate:     time.Now().AddDate(0, 6, 0),
	}
}

func TestApplyHHDiscount_NilGrant_AddsIneligibleEntry(t *testing.T) {
	eval := baseEval()
	applyHHDiscount(context.Background(), eval, nil)
	require.Len(t, eval.Discounts, 1)
	d := eval.Discounts[0]
	assert.Equal(t, DiscountTypeHH, d.Type)
	assert.False(t, d.Eligible)
	assert.False(t, d.Error)
	assert.Equal(t, 80.0, eval.FinalPrice.Amount, "final price unchanged")
	assert.Empty(t, eval.Explain, "explain unchanged when no grant")
}

func TestApplyHHDiscount_Eligible(t *testing.T) {
	eval := baseEval() // base = 80 NIS, final = 80 NIS
	applyHHDiscount(context.Background(), eval, hhGrant(1, 80))

	require.Len(t, eval.Discounts, 1)
	d := eval.Discounts[0]
	assert.Equal(t, DiscountTypeHH, d.Type)
	assert.Equal(t, 80.0, d.AmountPct)
	assert.True(t, d.Eligible)
	// 80 * (1 - 80/100) = 16 NIS
	assert.Equal(t, 16.0, eval.FinalPrice.Amount)
	assert.Equal(t, "NIS", eval.FinalPrice.Currency)
	require.Len(t, eval.Explain, 1)
}

func TestApplyHHDiscount_FullDiscount_FreeMembership(t *testing.T) {
	eval := baseEval()
	applyHHDiscount(context.Background(), eval, hhGrant(2, 100))

	require.Len(t, eval.Discounts, 1)
	assert.True(t, eval.Discounts[0].Eligible)
	assert.Equal(t, 0.0, eval.FinalPrice.Amount)
}

func TestApplyHHDiscount_NotEligible_PriceWouldBeHigher(t *testing.T) {
	// base = 80 NIS, but final has already been reduced to 36 NIS (e.g. donations discount)
	eval := baseEval()
	eval.FinalPrice = Price{Amount: 36.0, Currency: "NIS"}
	// HH 10% off base = 72 NIS which is > 36 NIS (current final)
	applyHHDiscount(context.Background(), eval, hhGrant(3, 10))

	require.Len(t, eval.Discounts, 1)
	assert.False(t, eval.Discounts[0].Eligible, "HH grant should not be applied when it yields a higher price")
	assert.Equal(t, 36.0, eval.FinalPrice.Amount, "final price should be unchanged")
	assert.Empty(t, eval.Explain, "explain unchanged when grant not applied")
}

func TestApplyHHDiscount_AuditProperties(t *testing.T) {
	eval := baseEval()
	grant := hhGrant(7, 50)
	grant.Type = common.HHGrantTypeHayal
	applyHHDiscount(context.Background(), eval, grant)

	require.Len(t, eval.Discounts, 1)
	var props hhDiscountAuditProps
	require.NoError(t, json.Unmarshal(eval.Discounts[0].Properties, &props))
	assert.Equal(t, 7, props.HHGrantID)
	assert.Equal(t, common.HHGrantTypeHayal, props.GrantType)
	assert.Equal(t, 50, props.DiscountPct)
	assert.Equal(t, grant.EndDate.Unix(), props.ExpiresAt.Unix())
}

// --- HHGrantProvider integration ---

func TestEvaluateV2Price_WithHHProvider_Applied(t *testing.T) {
	// No donations → final price = base (180 NIS). HH 80% off = 36 NIS < 180 NIS → applied.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	provider := hhGrantProviderFunc(func(_ context.Context, _ string) (*repo.HHGrant, error) {
		return hhGrant(1, 80), nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeDonations, eval.Discounts[0].Type)
	assert.Equal(t, DiscountTypeHH, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 36.0, eval.FinalPrice.Amount)
	assert.False(t, eval.HasDiscountErrors())
}

func TestEvaluateV2Price_WithHHProvider_NoActiveGrant(t *testing.T) {
	// Provider returns nil — HH entry is present but ineligible.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	provider := hhGrantProviderFunc(func(_ context.Context, _ string) (*repo.HHGrant, error) {
		return nil, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeHH, eval.Discounts[1].Type)
	assert.False(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 180.0, eval.FinalPrice.Amount)
}

func TestEvaluateV2Price_WithHHProvider_FetchError_RecordsErrorDiscount(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	provider := hhGrantProviderFunc(func(_ context.Context, _ string) (*repo.HHGrant, error) {
		return nil, assert.AnError
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeHH, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Error)
	assert.True(t, eval.HasDiscountErrors())
	assert.Equal(t, 180.0, eval.FinalPrice.Amount, "price unchanged on fetch error")
}

func TestEvaluateV2Price_HHAndManual_LowerPriceWins(t *testing.T) {
	// HH 50% off = 90 NIS; manual 80% off = 36 NIS → manual wins.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	hhProvider := hhGrantProviderFunc(func(_ context.Context, _ string) (*repo.HHGrant, error) {
		return hhGrant(1, 50), nil
	})
	mdProvider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return pctDiscount(2, 80), nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", mdProvider, hhProvider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 3)
	assert.Equal(t, DiscountTypeHH, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Eligible, "HH applied first against base")
	assert.Equal(t, DiscountTypeManual, eval.Discounts[2].Type)
	assert.True(t, eval.Discounts[2].Eligible, "manual beats the HH price")
	assert.Equal(t, 36.0, eval.FinalPrice.Amount)
}

func TestEvaluateV2Price_HHBetterThanManual_HHWins(t *testing.T) {
	// HH 80% off = 36 NIS; manual 50% off = 90 NIS → HH wins, manual not applied.
	server := noPriorityCustomersServer()
	defer server.Close()

	email := "primary@x.com"
	profileSvc := &stubProfileService{
		profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}},
	}
	hhProvider := hhGrantProviderFunc(func(_ context.Context, _ string) (*repo.HHGrant, error) {
		return hhGrant(1, 80), nil
	})
	mdProvider := manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return pctDiscount(2, 50), nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", mdProvider, hhProvider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 3)
	assert.True(t, eval.Discounts[1].Eligible, "HH applied")
	assert.False(t, eval.Discounts[2].Eligible, "manual yields a higher price than HH")
	assert.Equal(t, 36.0, eval.FinalPrice.Amount)
}
