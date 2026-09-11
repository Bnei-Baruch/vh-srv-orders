package pricing

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	pkgmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// manualDiscountProviderFunc is a function adapter that implements repo.ManualDiscountProvider.
type manualDiscountProviderFunc func(ctx context.Context, keycloakID string) (*repo.ManualDiscount, error)

func (f manualDiscountProviderFunc) GetActiveManualDiscount(ctx context.Context, keycloakID string) (*repo.ManualDiscount, error) {
	return f(ctx, keycloakID)
}

func TestResolve_ManualDiscount_DBError_ReturnsError(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()

	profilesMock := pkgmocks.NewMockProfileService(t)
	profilesMock.EXPECT().GetProfileByKeycloakID(mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	r := NewPriceResolver(profilesMock, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID)
	dbErr := fmt.Errorf("DB connection lost")
	r.SetManualDiscountProvider(manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return nil, dbErr
	}))

	account := &repo.Account{
		ID:      1,
		Country: null.StringFrom("IL"), // v2-eligible
		UserKey: null.StringFrom("kc-test"),
		Email:   null.StringFrom("user@example.com"),
	}

	_, err := r.Resolve(context.Background(), account)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDonationFetch)
}

func TestResolve_ManualDiscount_NoProvider_V2Succeeds(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()

	profilesMock := pkgmocks.NewMockProfileService(t)
	profilesMock.EXPECT().GetProfileByKeycloakID(mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	r := NewPriceResolver(profilesMock, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID)
	// No discount provider set — v2 pricing should complete normally.

	account := &repo.Account{
		ID:      2,
		Country: null.StringFrom("IL"),
		UserKey: null.StringFrom("kc-test2"),
		Email:   null.StringFrom("user2@example.com"),
	}

	result, err := r.Resolve(context.Background(), account)

	require.NoError(t, err)
	assert.Equal(t, "v2", result.PricingVersion)
	assert.NotNil(t, result.V2Evaluation)
}

func TestResolve_ManualDiscount_NoActiveDiscount_V2Succeeds(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()

	profilesMock := pkgmocks.NewMockProfileService(t)
	profilesMock.EXPECT().GetProfileByKeycloakID(mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	r := NewPriceResolver(profilesMock, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID)
	// Provider returns nil discount (no active discount for this user).
	r.SetManualDiscountProvider(manualDiscountProviderFunc(func(_ context.Context, _ string) (*repo.ManualDiscount, error) {
		return nil, nil
	}))

	account := &repo.Account{
		ID:      3,
		Country: null.StringFrom("IL"),
		UserKey: null.StringFrom("kc-test3"),
		Email:   null.StringFrom("user3@example.com"),
	}

	result, err := r.Resolve(context.Background(), account)

	require.NoError(t, err)
	assert.Equal(t, "v2", result.PricingVersion)
	// No active manual discount — both donations and manual entries are present.
	require.Len(t, result.V2Evaluation.Discounts, 2)
	assert.Equal(t, DiscountTypeDonations, result.V2Evaluation.Discounts[0].Type)
	assert.Equal(t, DiscountTypeManual, result.V2Evaluation.Discounts[1].Type)
	assert.False(t, result.V2Evaluation.Discounts[1].Eligible)
}
