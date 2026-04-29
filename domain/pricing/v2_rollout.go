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
// Current exclusions: US, UK, EU-27, Turkey, Russia, Ukraine, Canada,
// Latin America and the Caribbean.
var v2ExcludedMajorMarkets = map[string]bool{
	// United States
	"US": true,

	// United Kingdom
	"GB": true,

	// Canada
	"CA": true,

	// Latin America and the Caribbean
	"AR": true, // Argentina
	"BO": true, // Bolivia
	"BR": true, // Brazil
	"BZ": true, // Belize
	"CL": true, // Chile
	"CO": true, // Colombia
	"CR": true, // Costa Rica
	"CU": true, // Cuba
	"DO": true, // Dominican Republic
	"EC": true, // Ecuador
	"GT": true, // Guatemala
	"GY": true, // Guyana
	"HN": true, // Honduras
	"HT": true, // Haiti
	"JM": true, // Jamaica
	"MX": true, // Mexico
	"NI": true, // Nicaragua
	"PA": true, // Panama
	"PE": true, // Peru
	"PY": true, // Paraguay
	"SR": true, // Suriname
	"SV": true, // El Salvador
	"TT": true, // Trinidad and Tobago
	"UY": true, // Uruguay
	"VE": true, // Venezuela

	// EU-27 member states
	"AT": true, // Austria
	"BE": true, // Belgium
	"BG": true, // Bulgaria
	"HR": true, // Croatia
	"CY": true, // Cyprus
	"CZ": true, // Czech Republic
	"DK": true, // Denmark
	"EE": true, // Estonia
	"FI": true, // Finland
	"FR": true, // France
	"DE": true, // Germany
	"GR": true, // Greece
	"HU": true, // Hungary
	"IE": true, // Ireland
	"IT": true, // Italy
	"LV": true, // Latvia
	"LT": true, // Lithuania
	"LU": true, // Luxembourg
	"MT": true, // Malta
	"NL": true, // Netherlands
	"PL": true, // Poland
	"PT": true, // Portugal
	"RO": true, // Romania
	"SK": true, // Slovakia
	"SI": true, // Slovenia
	"ES": true, // Spain
	"SE": true, // Sweden

	// Other major markets
	"TR": true, // Turkey
	"RU": true, // Russia
	"UA": true, // Ukraine
	"NO": true, // Norway
	"CH": true, // Switzerland
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
