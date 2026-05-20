package pricing

import (
	"fmt"
	"strings"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// ValidateConfig returns a single error listing every required env var that is
// missing for v2 pricing. Call once from a command entry point and fail fast
// (utils.LogFatal) on error — hot paths trust that the config is valid.
func ValidateConfig() error {
	var missing []string
	if common.Config.PriorityBaseURL == "" {
		missing = append(missing, "PRIORITY_BASE_URL")
	}
	if common.Config.AccountingServiceUrl == "" {
		missing = append(missing, "ACCOUNTING_SERVICE_URL")
	}
	if common.Config.QuickbooksCompanyID == "" {
		missing = append(missing, "QUICKBOOKS_COMPANY_ID")
	}
	if len(missing) > 0 {
		return fmt.Errorf("v2 pricing requires env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}
