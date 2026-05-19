package pricing

import (
	"strings"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Price is a bare amount and currency with no pricing-tier metadata.
type Price struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// CountryBasePrice is a Price with a pricing-tier group attached.
type CountryBasePrice struct {
	Price
	Group string `json:"group"`
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
	"IT": "Medium", // Italy
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
	"RE": "Low",    // Reunion
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

	// Territories and dependencies — groups not yet assigned
	"AQ": "Undefined", // Antarctica
	"AX": "Undefined", // Alland Islands
	"BL": "Undefined", // Saint Barthelemy
	"BM": "Undefined", // Bermuda
	"BV": "Undefined", // Bouvet Island
	"CC": "Undefined", // Cocos (Keeling) Islands
	"CK": "Undefined", // Cook Islands
	"CW": "Undefined", // Curacao
	"CX": "Undefined", // Christmas Island
	"EH": "Undefined", // Western Sahara
	"FK": "Undefined", // Falkland Islands (Malvinas)
	"FO": "Undefined", // Faroe Islands
	"GF": "Undefined", // French Guiana
	"GG": "Undefined", // Guernsey
	"GI": "Undefined", // Gibraltar
	"GL": "Undefined", // Greenland
	"GP": "Undefined", // Guadeloupe
	"GS": "Undefined", // South Georgia and the South Sandwich Islands
	"GU": "Undefined", // Guam
	"HM": "Undefined", // Heard Island and McDonald Islands
	"IM": "Undefined", // Isle of Man
	"IO": "Undefined", // British Indian Ocean Territory
	"JE": "Undefined", // Jersey
	"MF": "Undefined", // Saint Martin (French part)
	"MP": "Undefined", // Northern Mariana Islands
	"MQ": "Undefined", // Martinique
	"MS": "Undefined", // Montserrat
	"NC": "Undefined", // New Caledonia
	"NF": "Undefined", // Norfolk Island
	"NU": "Undefined", // Niue
	"PF": "Undefined", // French Polynesia
	"PM": "Undefined", // Saint Pierre and Miquelon
	"PN": "Undefined", // Pitcairn
	"PR": "Undefined", // Puerto Rico
	"SH": "Undefined", // Saint Helena
	"SJ": "Undefined", // Svalbard and Jan Mayen
	"SS": "Undefined", // South Sudan
	"SX": "Undefined", // Sint Maarten (Dutch part)
	"TC": "Undefined", // Turks and Caicos Islands
	"TF": "Undefined", // French Southern Territories
	"TK": "Undefined", // Tokelau
	"VG": "Undefined", // British Virgin Islands
	"VI": "Undefined", // US Virgin Islands
	"WF": "Undefined", // Wallis and Futuna
	"YT": "Undefined", // Mayotte
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
// Key format: "CURRENCY-GROUP" (e.g., "NIS-Low", "EUR-Medium", "USD-High")
var priceByCurrencyAndGroup = map[string]float64{
	// NIS prices
	common.CurrencyNIS + "-Low":    60.0,
	common.CurrencyNIS + "-Medium": 90.0,
	common.CurrencyNIS + "-High":   180.0,

	// EUR prices
	common.CurrencyEUR + "-Low":    20.0,
	common.CurrencyEUR + "-Medium": 30.0,
	common.CurrencyEUR + "-High":   50.0,

	// USD prices
	common.CurrencyUSD + "-Low":    20.0,
	common.CurrencyUSD + "-Medium": 35.0,
	common.CurrencyUSD + "-High":   55.0,
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
		Price: Price{Amount: amount, Currency: currency},
		Group: group,
	}
}

// countryToName maps ISO-3166 alpha-2 country codes to human-readable names.
var countryToName = map[string]string{
	"AD": "Andorra",
	"AE": "United Arab Emirates",
	"AF": "Afghanistan",
	"AG": "Antigua and Barbuda",
	"AI": "Anguilla",
	"AL": "Albania",
	"AS": "American Samoa",
	"AW": "Aruba",
	"AM": "Armenia",
	"AO": "Angola",
	"AR": "Argentina",
	"AT": "Austria",
	"AU": "Australia",
	"AZ": "Azerbaijan",
	"BA": "Bosnia and Herzegovina",
	"BB": "Barbados",
	"BD": "Bangladesh",
	"BE": "Belgium",
	"BF": "Burkina Faso",
	"BG": "Bulgaria",
	"BH": "Bahrain",
	"BI": "Burundi",
	"BJ": "Benin",
	"BN": "Brunei Darussalam",
	"BO": "Bolivia",
	"BR": "Brazil",
	"BS": "Bahamas",
	"BT": "Bhutan",
	"BW": "Botswana",
	"BY": "Belarus",
	"BZ": "Belize",
	"CA": "Canada",
	"CD": "Congo, Democratic Republic of the",
	"CF": "Central African Republic",
	"CG": "Congo, Republic of the",
	"CH": "Switzerland",
	"CI": "Cote d'Ivoire",
	"CL": "Chile",
	"CM": "Cameroon",
	"CN": "China",
	"CO": "Colombia",
	"CR": "Costa Rica",
	"CU": "Cuba",
	"CV": "Cape Verde",
	"CY": "Cyprus",
	"CZ": "Czech Republic",
	"DE": "Germany",
	"DJ": "Djibouti",
	"DK": "Denmark",
	"DM": "Dominica",
	"DO": "Dominican Republic",
	"DZ": "Algeria",
	"EC": "Ecuador",
	"EE": "Estonia",
	"EG": "Egypt",
	"ER": "Eritrea",
	"ES": "Spain",
	"ET": "Ethiopia",
	"FI": "Finland",
	"FJ": "Fiji",
	"FM": "Micronesia, Federated States of",
	"FR": "France",
	"GA": "Gabon",
	"GB": "United Kingdom",
	"GD": "Grenada",
	"GE": "Georgia",
	"GH": "Ghana",
	"GM": "Gambia",
	"GN": "Guinea",
	"GQ": "Equatorial Guinea",
	"GR": "Greece",
	"GT": "Guatemala",
	"GW": "Guinea-Bissau",
	"GY": "Guyana",
	"HK": "Hong Kong",
	"HN": "Honduras",
	"HR": "Croatia",
	"HT": "Haiti",
	"HU": "Hungary",
	"ID": "Indonesia",
	"IE": "Ireland",
	"IL": "Israel",
	"IN": "India",
	"IQ": "Iraq",
	"IR": "Iran, Islamic Republic of",
	"IS": "Iceland",
	"IT": "Italy",
	"JM": "Jamaica",
	"JO": "Jordan",
	"JP": "Japan",
	"KE": "Kenya",
	"KG": "Kyrgyzstan",
	"KH": "Cambodia",
	"KI": "Kiribati",
	"KM": "Comoros",
	"KN": "Saint Kitts and Nevis",
	"KP": "Korea, Democratic People's Republic of",
	"KR": "Korea, Republic of",
	"KW": "Kuwait",
	"KY": "Cayman Islands",
	"KZ": "Kazakhstan",
	"LA": "Lao People's Democratic Republic",
	"LB": "Lebanon",
	"LC": "Saint Lucia",
	"LK": "Sri Lanka",
	"LI": "Liechtenstein",
	"LR": "Liberia",
	"LS": "Lesotho",
	"LT": "Lithuania",
	"LU": "Luxembourg",
	"LV": "Latvia",
	"LY": "Libya",
	"MA": "Morocco",
	"MC": "Monaco",
	"MD": "Moldova, Republic of",
	"ME": "Montenegro",
	"MG": "Madagascar",
	"MH": "Marshall Islands",
	"MK": "Macedonia, the Former Yugoslav Republic of",
	"ML": "Mali",
	"MM": "Myanmar",
	"MN": "Mongolia",
	"MO": "Macao",
	"MR": "Mauritania",
	"MT": "Malta",
	"MU": "Mauritius",
	"MV": "Maldives",
	"MW": "Malawi",
	"MX": "Mexico",
	"MY": "Malaysia",
	"MZ": "Mozambique",
	"NA": "Namibia",
	"NE": "Niger",
	"NG": "Nigeria",
	"NI": "Nicaragua",
	"NL": "Netherlands",
	"NO": "Norway",
	"NP": "Nepal",
	"NR": "Nauru",
	"NZ": "New Zealand",
	"OM": "Oman",
	"PA": "Panama",
	"PE": "Peru",
	"PG": "Papua New Guinea",
	"PH": "Philippines",
	"PK": "Pakistan",
	"PL": "Poland",
	"PS": "Palestine, State of",
	"PT": "Portugal",
	"PW": "Palau",
	"PY": "Paraguay",
	"QA": "Qatar",
	"RO": "Romania",
	"RS": "Serbia",
	"RU": "Russian Federation",
	"RW": "Rwanda",
	"SA": "Saudi Arabia",
	"SB": "Solomon Islands",
	"SC": "Seychelles",
	"SD": "Sudan",
	"SE": "Sweden",
	"SG": "Singapore",
	"SI": "Slovenia",
	"SK": "Slovakia",
	"SL": "Sierra Leone",
	"SM": "San Marino",
	"SN": "Senegal",
	"SO": "Somalia",
	"SR": "Suriname",
	"ST": "Sao Tome and Principe",
	"SV": "El Salvador",
	"SY": "Syrian Arab Republic",
	"SZ": "Swaziland",
	"TD": "Chad",
	"TG": "Togo",
	"TH": "Thailand",
	"TJ": "Tajikistan",
	"TL": "Timor-Leste",
	"TM": "Turkmenistan",
	"TN": "Tunisia",
	"TO": "Tonga",
	"TR": "Turkey",
	"TT": "Trinidad and Tobago",
	"TV": "Tuvalu",
	"TW": "Taiwan, Province of China",
	"TZ": "United Republic of Tanzania",
	"UA": "Ukraine",
	"UG": "Uganda",
	"US": "United States",
	"UY": "Uruguay",
	"UZ": "Uzbekistan",
	"VA": "Holy See (Vatican City State)",
	"VC": "Saint Vincent and the Grenadines",
	"VE": "Venezuela",
	"VN": "Vietnam",
	"VU": "Vanuatu",
	"WS": "Samoa",
	"XK": "Kosovo",
	"YE": "Yemen",
	"ZA": "South Africa",
	"ZM": "Zambia",
	"ZW": "Zimbabwe",

	// Territories and dependencies — names aligned with frontend
	"AQ": "Antarctica",
	"AX": "Alland Islands",
	"BL": "Saint Barthelemy",
	"BM": "Bermuda",
	"BV": "Bouvet Island",
	"CC": "Cocos (Keeling) Islands",
	"CK": "Cook Islands",
	"CW": "Curacao",
	"CX": "Christmas Island",
	"EH": "Western Sahara",
	"FK": "Falkland Islands (Malvinas)",
	"FO": "Faroe Islands",
	"GF": "French Guiana",
	"GG": "Guernsey",
	"GI": "Gibraltar",
	"GL": "Greenland",
	"GP": "Guadeloupe",
	"GS": "South Georgia and the South Sandwich Islands",
	"GU": "Guam",
	"HM": "Heard Island and McDonald Islands",
	"IM": "Isle of Man",
	"IO": "British Indian Ocean Territory",
	"JE": "Jersey",
	"MF": "Saint Martin (French part)",
	"MP": "Northern Mariana Islands",
	"MQ": "Martinique",
	"MS": "Montserrat",
	"NC": "New Caledonia",
	"NF": "Norfolk Island",
	"NU": "Niue",
	"PF": "French Polynesia",
	"PM": "Saint Pierre and Miquelon",
	"PN": "Pitcairn",
	"PR": "Puerto Rico",
	"RE": "Reunion",
	"SH": "Saint Helena",
	"SJ": "Svalbard and Jan Mayen",
	"SS": "South Sudan",
	"SX": "Sint Maarten (Dutch part)",
	"TC": "Turks and Caicos Islands",
	"TF": "French Southern Territories",
	"TK": "Tokelau",
	"VG": "British Virgin Islands",
	"VI": "US Virgin Islands",
	"WF": "Wallis and Futuna",
	"YT": "Mayotte",
}

// GetCountryName returns the human-readable country name for an ISO-3166 alpha-2 code.
// Returns the code itself if not found.
func GetCountryName(code string) string {
	code = strings.ToUpper(code)
	if name, ok := countryToName[code]; ok {
		return name
	}
	return code
}
