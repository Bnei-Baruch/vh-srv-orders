package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pkgmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
)

func TestGetMonthlyPrice_ResolvesV2(t *testing.T) {
	server := noPriorityCustomersServer()
	defer server.Close()

	profilesMock := pkgmocks.NewMockProfileService(t)
	profilesMock.EXPECT().GetProfileByKeycloakID(mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	res, err := GetMonthlyPrice(context.Background(), profilesMock, newPriorityTestClient(server.URL), notFoundAccountingClient(t), testQuickbooksCompanyID,
		1, "kc1", "user@example.com", "RU", nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "v2", res.PricingVersion.String)
	assert.NotNil(t, res.V2Details)
	assert.Equal(t, 31.0, res.Amount.Float64, "RU resolves to the v2 USD-Medium base")
}
