package pricing

import (
	"encoding/json"
	"fmt"

	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

// buildHHDiscount creates a Discount record for an active HH grant.
// The caller must verify the grant is not nil and not expired before calling.
func buildHHDiscount(hh *profiles.HHGrant) Discount {
	d := Discount{
		Type:     DiscountTypeHH,
		Eligible: true,
	}
	if hh.DiscountPct != nil {
		d.AmountPct = float64(*hh.DiscountPct)
	}
	propsJSON, _ := json.Marshal(HHDiscountProperties{ExpiresAt: hh.ExpiresAt})
	d.Properties = propsJSON
	return d
}

// hhExplainLine generates the explain line for an HH discount after resolveFinalPrice
// has set Applied. Returns a description of whether the grant was applied or lost to a
// better discount.
func hhExplainLine(d Discount) string {
	var hhProps HHDiscountProperties
	_ = json.Unmarshal(d.Properties, &hhProps)
	exp := hhProps.ExpiresAt.Format("Jan 2006")
	if d.Applied {
		return fmt.Sprintf("Help Haver: %d%% off (approved - expires %s) → applied", int(d.AmountPct), exp)
	}
	return fmt.Sprintf("Help Haver: %d%% off (approved - expires %s) → eligible, not applied (donations is better)", int(d.AmountPct), exp)
}
