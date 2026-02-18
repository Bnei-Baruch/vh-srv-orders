package repo

import (
	"context"
)

// Dry-run outcome buckets (deterministic from orderID):
// - h < 15 (15%): fail both terminals
// - h < 45 (30%): fail token, succeed EMV
// - h >= 45 (55%): succeed token
const (
	dryRunBucketFailBoth   = 15
	dryRunBucketFailToken  = 45
	dryRunPmxToken         = "t"
	dryRunPmxEMV           = "e"
	dryRunStatusSuccess    = "success"
	dryRunStatusDeclined   = "declined"
)

// DryRunChargeExecutor implements ChargeExecutor with no HTTP call.
// Returns success or declined deterministically per (orderID, pmx) so that
// 15% of orders fail both terminals, 30% fail token then succeed EMV, 55% succeed token.
type DryRunChargeExecutor struct{}

// NewDryRunChargeExecutor returns a charge executor for dry-run mode.
func NewDryRunChargeExecutor() *DryRunChargeExecutor {
	return &DryRunChargeExecutor{}
}

// Execute returns a simulated gateway response. No network call is made.
// Thread-safe: bucket is computed from orderID only (pure function).
func (e *DryRunChargeExecutor) Execute(ctx context.Context, request *RequestPayment, pmx string, orderID uint) (map[string]interface{}, error) {
	h := uint64(orderID) * 2654435761 % 100
	success := dryRunSuccess(h, pmx)
	status := dryRunStatusDeclined
	if success {
		status = dryRunStatusSuccess
	}
	return map[string]interface{}{"status": status}, nil
}

// dryRunSuccess returns whether the given (bucket, pmx) should simulate success.
// Bucket 0 (h < 15): both fail. Bucket 1 (h < 45): token fail, EMV success. Bucket 2 (h >= 45): token success.
func dryRunSuccess(h uint64, pmx string) bool {
	switch {
	case h < dryRunBucketFailBoth:
		return false
	case h < dryRunBucketFailToken:
		return pmx == dryRunPmxEMV
	default:
		return pmx == dryRunPmxToken
	}
}
