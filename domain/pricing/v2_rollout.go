package pricing

import "strings"

// v2Excluded temporarily excludes countries from v2 pricing for operational reasons
// (e.g., data quality issues, system maintenance).
var v2Excluded = map[string]bool{
	// No overrides yet.
}

// v2ExcludedMajorMarkets lists major markets whose donation data is not yet available in any
// connected accounting system. Remove countries as integrations expand.
//
// No exclusions remain — all major markets are on v2 (Russia was the last, now on v2;
// Russian offline payments continue via Robokasa, which bypasses pricing entirely).
var v2ExcludedMajorMarkets = map[string]bool{
	// No exclusions remain.
}

// V2Eligible returns true if the country should use v2 pricing.
// A country is eligible unless it is in the excluded major markets list or
// temporarily excluded via v2Excluded. Unknown or missing (empty/NULL) country
// codes are eligible and resolve to the highest USD tier via GetCountryBasePrice.
func V2Eligible(country string) bool {
	code := strings.ToUpper(country)
	if v2Excluded[code] {
		return false
	}
	if v2ExcludedMajorMarkets[code] {
		return false
	}
	return true
}
