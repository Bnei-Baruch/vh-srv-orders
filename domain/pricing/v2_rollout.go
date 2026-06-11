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
// Current exclusions: Russia.
// Europe (EU-27, UK, Norway, Switzerland, Western Balkans) and Turkey were opened
// when the European donations source was integrated.
var v2ExcludedMajorMarkets = map[string]bool{
	"RU": true, // Russia
}

// V2Eligible returns true if the country should use v2 pricing.
// A country is eligible when it is known, not in the excluded major markets list,
// and not temporarily excluded via v2Excluded.
func V2Eligible(country string) bool {
	if country == "" {
		return false
	}
	code := strings.ToUpper(country)
	if v2Excluded[code] {
		return false
	}
	if v2ExcludedMajorMarkets[code] {
		return false
	}
	return true
}
