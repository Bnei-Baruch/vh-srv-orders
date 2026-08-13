package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	pkgmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
)

// offlineV2Resolver builds a PriceResolver whose external dependencies all report
// "no data", so v2 evaluation resolves to the country base price without any real
// network calls. Used by charge-orchestration tests that only care about the charge
// pipeline succeeding, not about pricing internals.
func offlineV2Resolver(t *testing.T) *pricing.PriceResolver {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(priority.CustomerODataResponse{Value: []priority.Customer{}})
	}))
	t.Cleanup(server.Close)
	common.Config.PriorityBaseURL = server.URL

	profileMock := pkgmocks.NewMockProfileService(t)
	profileMock.EXPECT().GetProfileByKeycloakID(mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	acc := pkgmocks.NewMockAccountingService(t)
	acc.EXPECT().GetLastContributions(mock.Anything, mock.Anything, mock.Anything).
		Return(&accounting.ContributionsResult{Found: false}, nil).Maybe()
	acc.EXPECT().GetEuropeContributions(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, emails []string) (*accounting.EuropeContributionsResult, error) {
			res := &accounting.EuropeContributionsResult{LookbackMonths: 12}
			for _, email := range emails {
				res.Results = append(res.Results, accounting.EuropeContributionEntry{
					IdentifierType: "email", Identifier: email, Found: false,
				})
			}
			return res, nil
		}).Maybe()

	return pricing.NewPriceResolver(profileMock, priority.NewClient(), acc, "test-co")
}
