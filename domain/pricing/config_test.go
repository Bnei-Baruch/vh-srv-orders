package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func TestValidateConfig_AllSet_ReturnsNil(t *testing.T) {
	restore := withConfig(t, "http://priority", "http://accounting", "qb-co")
	defer restore()

	require.NoError(t, ValidateConfig())
}

func TestValidateConfig_PriorityMissing(t *testing.T) {
	restore := withConfig(t, "", "http://accounting", "qb-co")
	defer restore()

	err := ValidateConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
	assert.NotContains(t, err.Error(), "ACCOUNTING_SERVICE_URL")
	assert.NotContains(t, err.Error(), "QUICKBOOKS_COMPANY_ID")
}

func TestValidateConfig_AccountingURLMissing(t *testing.T) {
	restore := withConfig(t, "http://priority", "", "qb-co")
	defer restore()

	err := ValidateConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ACCOUNTING_SERVICE_URL")
	assert.NotContains(t, err.Error(), "PRIORITY_BASE_URL")
	assert.NotContains(t, err.Error(), "QUICKBOOKS_COMPANY_ID")
}

func TestValidateConfig_CompanyIDMissing(t *testing.T) {
	restore := withConfig(t, "http://priority", "http://accounting", "")
	defer restore()

	err := ValidateConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "QUICKBOOKS_COMPANY_ID")
	assert.NotContains(t, err.Error(), "PRIORITY_BASE_URL")
	assert.NotContains(t, err.Error(), "ACCOUNTING_SERVICE_URL")
}

func TestValidateConfig_AllMissing_ReportsAll(t *testing.T) {
	restore := withConfig(t, "", "", "")
	defer restore()

	err := ValidateConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRIORITY_BASE_URL")
	assert.Contains(t, err.Error(), "ACCOUNTING_SERVICE_URL")
	assert.Contains(t, err.Error(), "QUICKBOOKS_COMPANY_ID")
}

// withConfig snapshots and overrides the three v2-pricing env vars on the global config singleton.
// The returned func restores the previous values; defer it.
func withConfig(t *testing.T, priorityURL, accountingURL, companyID string) func() {
	t.Helper()
	origP, origA, origC := common.Config.PriorityBaseURL, common.Config.AccountingServiceUrl, common.Config.QuickbooksCompanyID
	common.Config.PriorityBaseURL = priorityURL
	common.Config.AccountingServiceUrl = accountingURL
	common.Config.QuickbooksCompanyID = companyID
	return func() {
		common.Config.PriorityBaseURL = origP
		common.Config.AccountingServiceUrl = origA
		common.Config.QuickbooksCompanyID = origC
	}
}
