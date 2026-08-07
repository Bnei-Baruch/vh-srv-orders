package pricing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/coupon"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type couponProviderFunc func(ctx context.Context, keycloakID string) ([]repo.ActiveCouponRedemption, error)

func (f couponProviderFunc) GetActiveCouponRedemptions(ctx context.Context, keycloakID string) ([]repo.ActiveCouponRedemption, error) {
	return f(ctx, keycloakID)
}

func pctCoupon(id int, code string, pct float64) repo.ActiveCouponRedemption {
	props, _ := json.Marshal(repo.CouponProperties{DiscountPct: &pct})
	return repo.ActiveCouponRedemption{
		RedemptionID: id, CouponID: id, Code: code, Type: coupon.TypePercent,
		Properties: null.JSONFrom(props), BenefitEnd: time.Now().Add(720 * time.Hour),
	}
}

func fixedCoupon(id int, code string, price float64, currency string) repo.ActiveCouponRedemption {
	props, _ := json.Marshal(repo.CouponProperties{FixedPrice: &price, Currency: &currency})
	return repo.ActiveCouponRedemption{
		RedemptionID: id, CouponID: id, Code: code, Type: coupon.TypeFixedPrice,
		Properties: null.JSONFrom(props), BenefitEnd: time.Now().Add(720 * time.Hour),
	}
}

// couponTestSetup returns a stub profile service and a no-donations Priority
// client so an IL account evaluates to the base 180 NIS before coupons apply.
func couponTestSetup(t *testing.T) (*stubProfileService, string) {
	t.Helper()
	email := "primary@x.com"
	return &stubProfileService{profiles: map[string]*profiles.Profile{"kc-1": {PrimaryEmail: &email}}}, email
}

func TestEvaluateV2Price_PercentCoupon_Applied(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return []repo.ActiveCouponRedemption{pctCoupon(1, "SUKKOT-AAAAA", 50)}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2) // donations + coupon
	assert.Equal(t, DiscountTypeCoupon, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 90.0, eval.FinalPrice.Amount)
	assert.Equal(t, common.CurrencyNIS, eval.FinalPrice.Currency)
}

func TestEvaluateV2Price_FixedPriceCoupon_MatchingCurrency_Applied(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return []repo.ActiveCouponRedemption{fixedCoupon(1, "FIX-AAAAA", 100, common.CurrencyNIS)}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.True(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 100.0, eval.FinalPrice.Amount)
}

func TestEvaluateV2Price_FixedPriceCoupon_ForeignCurrency_ConvertedAndApplied(t *testing.T) {
	// Coupons are not country-scoped: a USD fixed_price coupon applies to an IL
	// (NIS) member. It is ranked in NIS (10 USD × 3.1 = 31 NIS < 180) and, like a
	// fixed_price manual discount, keeps its own currency in the final price.
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return []repo.ActiveCouponRedemption{fixedCoupon(1, "USD-AAAAA", 10, "USD")}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.True(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 10.0, eval.FinalPrice.Amount)
	assert.Equal(t, "USD", eval.FinalPrice.Currency)
	// Effective percentage is rounded to two decimals: (1 - 31/180) * 100 = 82.78.
	assert.Equal(t, 82.78, eval.Discounts[1].AmountPct)
}

func TestEvaluateV2Price_MultipleCoupons_LowestWins(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return []repo.ActiveCouponRedemption{
			pctCoupon(1, "SMALL-AAAAA", 25),                     // 135 NIS
			fixedCoupon(2, "FIX-BBBBB", 60, common.CurrencyNIS), // 60 NIS — best
			pctCoupon(3, "BIG-CCCCC", 40),                       // 108 NIS
		}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 4) // donations + 3 coupons
	assert.Equal(t, 60.0, eval.FinalPrice.Amount)
	eligible := 0
	for _, d := range eval.Discounts {
		if d.Type == DiscountTypeCoupon && d.Eligible {
			eligible++
		}
	}
	assert.Equal(t, 1, eligible, "only the lowest coupon wins; no stacking")
}

func TestEvaluateV2Price_Coupon_FetchError_RecordsErrorDiscount(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return nil, assert.AnError
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	require.Len(t, eval.Discounts, 2)
	assert.Equal(t, DiscountTypeCoupon, eval.Discounts[1].Type)
	assert.True(t, eval.Discounts[1].Error)
	assert.True(t, eval.HasDiscountErrors(), "coupon lookup error blocks the charge (decision #11)")
	assert.Equal(t, 180.0, eval.FinalPrice.Amount)
}

func TestEvaluateV2Price_Coupon_MalformedProperties_RecordsErrorDiscount(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return []repo.ActiveCouponRedemption{
			{CouponID: 1, RedemptionID: 1, Code: "BAD", Type: coupon.TypePercent,
				Properties: null.JSONFrom([]byte(`{}`)), // missing discount_pct
				BenefitEnd: time.Now().Add(720 * time.Hour)},
		}, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	couponDiscount := eval.Discounts[len(eval.Discounts)-1]
	assert.True(t, couponDiscount.Error, "malformed coupon properties should set Error=true")
	assert.False(t, couponDiscount.Eligible)
	assert.True(t, eval.HasDiscountErrors(), "malformed coupon blocks the charge (decision #11)")
	assert.Equal(t, 180.0, eval.FinalPrice.Amount, "price must not change on a malformed coupon")
}

func TestEvaluateV2Price_Coupon_LookupUsesPrimaryKeycloakOnly(t *testing.T) {
	// A spouse's redemptions must never leak into the primary's evaluation:
	// the coupon lookup is keyed strictly on the redeemer's keycloak (§2).
	server := noPriorityCustomersServer()
	defer server.Close()

	primaryEmail, spouseEmail := "primary@x.com", "spouse@x.com"
	spouseKC := "kc-2"
	profileSvc := &stubProfileService{profiles: map[string]*profiles.Profile{
		"kc-1": {PrimaryEmail: &primaryEmail, SpouseKeycloakID: &spouseKC},
		"kc-2": {PrimaryEmail: &spouseEmail},
	}}

	var lookedUp []string
	provider := couponProviderFunc(func(_ context.Context, keycloakID string) ([]repo.ActiveCouponRedemption, error) {
		lookedUp = append(lookedUp, keycloakID)
		return nil, nil
	})

	_, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", primaryEmail, "IL", nil, nil, provider)
	require.NoError(t, err)
	assert.Equal(t, []string{"kc-1"}, lookedUp, "coupon lookup must use only the primary keycloak, never the spouse")
}

func TestEvaluateV2Price_NoRedemptions_EmitsIneligibleCouponRow(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()
	profileSvc, email := couponTestSetup(t)

	provider := couponProviderFunc(func(_ context.Context, _ string) ([]repo.ActiveCouponRedemption, error) {
		return nil, nil
	})

	eval, err := EvaluateV2Price(context.Background(), profileSvc, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID, 10, "kc-1", email, "IL", nil, nil, provider)
	require.NoError(t, err)

	// A coupon row is always emitted (ineligible when the member has no active
	// redemption) for consistency with donations/HH/manual.
	require.Len(t, eval.Discounts, 2) // donations + ineligible coupon
	assert.Equal(t, DiscountTypeCoupon, eval.Discounts[1].Type)
	assert.False(t, eval.Discounts[1].Eligible)
	assert.Equal(t, 180.0, eval.FinalPrice.Amount)
}
