package pricing

import (
	"strings"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// CountryBasePrice represents the base price information for a country
type CountryBasePrice struct {
	Amount   float64
	Currency string
	Group    string
}

// countryToGroup maps country codes to their pricing groups (Low, Medium, High)
// Data extracted from the spreadsheet
var countryToGroup = map[string]string{
	"AD": "High",   // Andorra
	"AE": "High",   // United Arab Emirates
	"AF": "Low",    // Afghanistan
	"AG": "High",   // Antigua and Barbuda
	"AI": "High",   // Anguilla
	"AL": "Medium", // Albania
	"AN": "High",   // Netherlands Antilles
	"AS": "High",   // American Samoa
	"AW": "High",   // Aruba
	"AM": "Low",    // Armenia
	"AO": "Low",    // Angola
	"AR": "Low",    // Argentina
	"AT": "High",   // Austria
	"AU": "High",   // Australia
	"AZ": "Low",    // Azerbaijan
	"BA": "Medium", // Bosnia and Herzegovina
	"BB": "High",   // Barbados
	"BD": "Low",    // Bangladesh
	"BE": "High",   // Belgium
	"BF": "Low",    // Burkina Faso
	"BG": "Medium", // Bulgaria
	"BH": "High",   // Bahrain
	"BI": "Low",    // Burundi
	"BJ": "Low",    // Benin
	"BN": "High",   // Brunei
	"BO": "Low",    // Bolivia
	"BR": "Low",    // Brazil
	"BS": "High",   // Bahamas
	"BT": "Low",    // Bhutan
	"BW": "Medium", // Botswana
	"BY": "Low",    // Belarus
	"BZ": "Medium", // Belize
	"CA": "High",   // Canada
	"CD": "Low",    // Democratic Republic of the Congo
	"CF": "Low",    // Central African Republic
	"CG": "Low",    // Republic of the Congo
	"CH": "High",   // Switzerland
	"CI": "Low",    // Côte d'Ivoire
	"CL": "Medium", // Chile
	"CM": "Low",    // Cameroon
	"CN": "Medium", // China
	"CO": "Low",    // Colombia
	"CR": "Medium", // Costa Rica
	"CU": "Low",    // Cuba
	"CV": "Low",    // Cabo Verde
	"CY": "High",   // Cyprus
	"CZ": "Medium", // Czech Republic
	"DE": "High",   // Germany
	"DJ": "Low",    // Djibouti
	"DK": "High",   // Denmark
	"DM": "Medium", // Dominica
	"DO": "Medium", // Dominican Republic
	"DZ": "Medium", // Algeria
	"EC": "Low",    // Ecuador
	"EE": "Medium", // Estonia
	"EG": "Medium", // Egypt
	"ER": "Low",    // Eritrea
	"ES": "High",   // Spain
	"ET": "Low",    // Ethiopia
	"FI": "High",   // Finland
	"FJ": "Medium", // Fiji
	"FM": "Low",    // Micronesia
	"FR": "High",   // France
	"GA": "Medium", // Gabon
	"GB": "High",   // United Kingdom
	"GD": "Medium", // Grenada
	"GE": "Low",    // Georgia
	"GH": "Low",    // Ghana
	"GM": "Low",    // Gambia
	"GN": "Low",    // Guinea
	"GQ": "Medium", // Equatorial Guinea
	"GR": "High",   // Greece
	"GT": "Low",    // Guatemala
	"GW": "Low",    // Guinea-Bissau
	"GY": "High",   // Guyana
	"HK": "High",   // Hong Kong
	"HN": "Low",    // Honduras
	"HR": "Medium", // Croatia
	"HT": "Low",    // Haiti
	"HU": "Medium", // Hungary
	"ID": "Low",    // Indonesia
	"IE": "High",   // Ireland
	"IL": "High",   // Israel
	"IN": "Low",    // India
	"IQ": "Medium", // Iraq
	"IR": "Medium", // Iran
	"IS": "High",   // Iceland
	"IT": "High",   // Italy
	"JM": "Medium", // Jamaica
	"JO": "Low",    // Jordan
	"JP": "High",   // Japan
	"KE": "Low",    // Kenya
	"KG": "Low",    // Kyrgyzstan
	"KH": "Low",    // Cambodia
	"KI": "Low",    // Kiribati
	"KM": "Low",    // Comoros
	"KN": "High",   // Saint Kitts and Nevis
	"KP": "Low",    // North Korea
	"KR": "High",   // South Korea
	"KW": "High",   // Kuwait
	"KY": "High",   // Cayman Islands
	"KZ": "Medium", // Kazakhstan
	"LA": "Low",    // Laos
	"LB": "Medium", // Lebanon
	"LC": "Medium", // Saint Lucia
	"LK": "Medium", // Sri Lanka
	"LI": "High",   // Liechtenstein
	"LR": "Low",    // Liberia
	"LS": "Low",    // Lesotho
	"LT": "Medium", // Lithuania
	"LU": "High",   // Luxembourg
	"LV": "Medium", // Latvia
	"LY": "Medium", // Libya
	"MA": "Low",    // Morocco
	"MC": "High",   // Monaco
	"MD": "Low",    // Moldova
	"ME": "Medium", // Montenegro
	"MG": "Low",    // Madagascar
	"MH": "Low",    // Marshall Islands
	"MK": "Medium", // North Macedonia
	"ML": "Low",    // Mali
	"MM": "Low",    // Myanmar
	"MN": "Low",    // Mongolia
	"MO": "High",   // Macao
	"MR": "Low",    // Mauritania
	"MT": "High",   // Malta
	"MU": "Medium", // Mauritius
	"MV": "Medium", // Maldives
	"MW": "Low",    // Malawi
	"MX": "Low",    // Mexico
	"MY": "High",   // Malaysia
	"MZ": "Low",    // Mozambique
	"NA": "Medium", // Namibia
	"NE": "Low",    // Niger
	"NG": "Low",    // Nigeria
	"NI": "Low",    // Nicaragua
	"NL": "High",   // Netherlands
	"NO": "High",   // Norway
	"NP": "Low",    // Nepal
	"NR": "Medium", // Nauru
	"NZ": "High",   // New Zealand
	"OM": "High",   // Oman
	"PA": "Medium", // Panama
	"PE": "Low",    // Peru
	"PG": "Low",    // Papua New Guinea
	"PH": "Low",    // Philippines
	"PK": "Low",    // Pakistan
	"PL": "Medium", // Poland
	"PS": "Low",    // Palestine
	"PT": "Medium", // Portugal
	"PW": "Medium", // Palau
	"PY": "Low",    // Paraguay
	"QA": "High",   // Qatar
	"RO": "Medium", // Romania
	"RS": "Medium", // Serbia
	"RU": "Medium", // Russia
	"RW": "Low",    // Rwanda
	"SA": "High",   // Saudi Arabia
	"SB": "Low",    // Solomon Islands
	"SC": "Medium", // Seychelles
	"SD": "Low",    // Sudan
	"SE": "High",   // Sweden
	"SG": "High",   // Singapore
	"SI": "Medium", // Slovenia
	"SK": "Medium", // Slovakia
	"SL": "Low",    // Sierra Leone
	"SM": "High",   // San Marino
	"SN": "Low",    // Senegal
	"SO": "Low",    // Somalia
	"SR": "Medium", // Suriname
	"ST": "Low",    // Sao Tome and Principe
	"SV": "Low",    // El Salvador
	"SY": "Low",    // Syria
	"SZ": "Low",    // Eswatini
	"TD": "Low",    // Chad
	"TG": "Low",    // Togo
	"TH": "Medium", // Thailand
	"TJ": "Low",    // Tajikistan
	"TL": "Low",    // Timor-Leste
	"TM": "Low",    // Turkmenistan
	"TN": "Low",    // Tunisia
	"TO": "Low",    // Tonga
	"TR": "Medium", // Turkey
	"TT": "High",   // Trinidad and Tobago
	"TV": "Low",    // Tuvalu
	"TW": "High",   // Taiwan
	"TZ": "Low",    // Tanzania
	"UA": "Low",    // Ukraine
	"UG": "Low",    // Uganda
	"US": "High",   // United States
	"UY": "Medium", // Uruguay
	"UZ": "Low",    // Uzbekistan
	"VA": "Medium", // Vatican City
	"VC": "Medium", // Saint Vincent and the Grenadines
	"VE": "Low",    // Venezuela
	"VN": "Low",    // Vietnam
	"VU": "Low",    // Vanuatu
	"WS": "Low",    // Samoa
	"XK": "Medium", // Kosovo
	"YE": "Low",    // Yemen
	"ZA": "Medium", // South Africa
	"ZM": "Low",    // Zambia
	"ZW": "Low",    // Zimbabwe
}

// countryToCurrency maps country codes to their currencies
// Israel → ILS, European countries → EUR, Rest → USD
var countryToCurrency = map[string]string{
	// Israel
	"IL": common.CurrencyNIS,

	// European countries (EU member states and other European countries)
	"AT": common.CurrencyEUR, // Austria
	"BE": common.CurrencyEUR, // Belgium
	"BG": common.CurrencyEUR, // Bulgaria
	"HR": common.CurrencyEUR, // Croatia
	"CY": common.CurrencyEUR, // Cyprus
	"CZ": common.CurrencyEUR, // Czech Republic
	"DK": common.CurrencyEUR, // Denmark
	"EE": common.CurrencyEUR, // Estonia
	"FI": common.CurrencyEUR, // Finland
	"FR": common.CurrencyEUR, // France
	"DE": common.CurrencyEUR, // Germany
	"GR": common.CurrencyEUR, // Greece
	"HU": common.CurrencyEUR, // Hungary
	"IE": common.CurrencyEUR, // Ireland
	"IT": common.CurrencyEUR, // Italy
	"LV": common.CurrencyEUR, // Latvia
	"LT": common.CurrencyEUR, // Lithuania
	"LU": common.CurrencyEUR, // Luxembourg
	"MT": common.CurrencyEUR, // Malta
	"NL": common.CurrencyEUR, // Netherlands
	"PL": common.CurrencyEUR, // Poland
	"PT": common.CurrencyEUR, // Portugal
	"RO": common.CurrencyEUR, // Romania
	"SK": common.CurrencyEUR, // Slovakia
	"SI": common.CurrencyEUR, // Slovenia
	"ES": common.CurrencyEUR, // Spain
	"SE": common.CurrencyEUR, // Sweden
	"CH": common.CurrencyEUR, // Switzerland
	"NO": common.CurrencyEUR, // Norway
	"IS": common.CurrencyEUR, // Iceland
	"GB": common.CurrencyEUR, // United Kingdom
	"LI": common.CurrencyEUR, // Liechtenstein
}

// priceByCurrencyAndGroup maps (Currency, Group) combinations to base prices
// Key format: "CURRENCY-GROUP" (e.g., "ILS-Low", "EUR-Medium", "USD-High")
// Prices will be added manually based on the spreadsheet data
var priceByCurrencyAndGroup = map[string]float64{
	// ILS prices
	"NIS-Low":    60.0,
	"NIS-Medium": 90.0,
	"NIS-High":   180.0,

	// EUR prices
	"EUR-Low":    20.0,
	"EUR-Medium": 30.0,
	"EUR-High":   50.0,

	// USD prices
	"USD-Low":    20.0,
	"USD-Medium": 35.0,
	"USD-High":   55.0,
}

// GetCountryBasePrice returns the base price information for a given country code
// Input: country code (ISO 2-letter code, e.g., "IL", "US", "DE")
// Output: CountryBasePrice struct with Amount, Currency, and Group
func GetCountryBasePrice(countryCode string) CountryBasePrice {
	// Normalize country code to uppercase for case-insensitive lookup
	code := strings.ToUpper(countryCode)

	// Get the group for this country (Low, Medium, or High)
	group, exists := countryToGroup[code]
	if !exists {
		group = "High" // Default group if not found
	}

	// Get currency for this country (ILS, EUR, or USD)
	currency, exists := countryToCurrency[code]
	if !exists {
		currency = common.CurrencyUSD // Default currency for rest of the world
	}

	// Get base price based on (Currency, Group) combination
	priceKey := currency + "-" + group
	amount, exists := priceByCurrencyAndGroup[priceKey]
	if !exists {
		amount = 55.0 // Default to 0 if combination not found
	}

	return CountryBasePrice{
		Amount:   amount,
		Currency: currency,
		Group:    group,
	}
}
