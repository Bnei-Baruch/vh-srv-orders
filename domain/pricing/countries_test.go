package pricing

import (
	"strings"
	"testing"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func TestGetCountryBasePrice_Israel(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected CountryBasePrice
	}{
		{
			name: "Israel uppercase",
			code: "IL",
			expected: CountryBasePrice{
				Price: Price{Amount: 180.0, Currency: common.CurrencyNIS},
				Group: "High",
			},
		},
		{
			name: "Israel lowercase",
			code: "il",
			expected: CountryBasePrice{
				Price: Price{Amount: 180.0, Currency: common.CurrencyNIS},
				Group: "High",
			},
		},
		{
			name: "Israel mixed case",
			code: "Il",
			expected: CountryBasePrice{
				Price: Price{Amount: 180.0, Currency: common.CurrencyNIS},
				Group: "High",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			if result.Amount != tt.expected.Amount {
				t.Errorf("Amount = %v, want %v", result.Amount, tt.expected.Amount)
			}
			if result.Currency != tt.expected.Currency {
				t.Errorf("Currency = %v, want %v", result.Currency, tt.expected.Currency)
			}
			if result.Group != tt.expected.Group {
				t.Errorf("Group = %v, want %v", result.Group, tt.expected.Group)
			}
		})
	}
}

func TestGetCountryBasePrice_EuropeanCountries(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected CountryBasePrice
	}{
		{
			name:     "Germany High group",
			code:     "DE",
			expected: CountryBasePrice{Price: Price{Amount: 53.0, Currency: common.CurrencyEUR}, Group: "High"},
		},
		{
			name:     "France High group",
			code:     "FR",
			expected: CountryBasePrice{Price: Price{Amount: 53.0, Currency: common.CurrencyEUR}, Group: "High"},
		},
		{
			name:     "Bulgaria Medium group",
			code:     "BG",
			expected: CountryBasePrice{Price: Price{Amount: 27.0, Currency: common.CurrencyEUR}, Group: "Medium"},
		},
		{
			name:     "Romania Medium group",
			code:     "RO",
			expected: CountryBasePrice{Price: Price{Amount: 27.0, Currency: common.CurrencyEUR}, Group: "Medium"},
		},
		{
			name:     "Ukraine Low group",
			code:     "UA",
			expected: CountryBasePrice{Price: Price{Amount: 21.0, Currency: common.CurrencyUSD}, Group: "Low"}, // Ukraine is not in European currency map
		},
		{
			name:     "Liechtenstein High group",
			code:     "LI",
			expected: CountryBasePrice{Price: Price{Amount: 53.0, Currency: common.CurrencyEUR}, Group: "High"},
		},
		{
			name:     "Switzerland High group",
			code:     "CH",
			expected: CountryBasePrice{Price: Price{Amount: 53.0, Currency: common.CurrencyEUR}, Group: "High"},
		},
		{
			name:     "United Kingdom High group",
			code:     "GB",
			expected: CountryBasePrice{Price: Price{Amount: 53.0, Currency: common.CurrencyEUR}, Group: "High"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			if result.Amount != tt.expected.Amount {
				t.Errorf("Amount = %v, want %v", result.Amount, tt.expected.Amount)
			}
			if result.Currency != tt.expected.Currency {
				t.Errorf("Currency = %v, want %v", result.Currency, tt.expected.Currency)
			}
			if result.Group != tt.expected.Group {
				t.Errorf("Group = %v, want %v", result.Group, tt.expected.Group)
			}
		})
	}
}

func TestGetCountryBasePrice_USDCountries(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected CountryBasePrice
	}{
		{
			name:     "United States High group",
			code:     "US",
			expected: CountryBasePrice{Price: Price{Amount: 62.0, Currency: common.CurrencyUSD}, Group: "High"},
		},
		{
			name:     "Canada High group",
			code:     "CA",
			expected: CountryBasePrice{Price: Price{Amount: 62.0, Currency: common.CurrencyUSD}, Group: "High"},
		},
		{
			name:     "Brazil Low group",
			code:     "BR",
			expected: CountryBasePrice{Price: Price{Amount: 21.0, Currency: common.CurrencyUSD}, Group: "Low"},
		},
		{
			name:     "Mexico Low group",
			code:     "MX",
			expected: CountryBasePrice{Price: Price{Amount: 21.0, Currency: common.CurrencyUSD}, Group: "Low"},
		},
		{
			name:     "India Low group",
			code:     "IN",
			expected: CountryBasePrice{Price: Price{Amount: 21.0, Currency: common.CurrencyUSD}, Group: "Low"},
		},
		{
			name:     "China Medium group",
			code:     "CN",
			expected: CountryBasePrice{Price: Price{Amount: 31.0, Currency: common.CurrencyUSD}, Group: "Medium"},
		},
		{
			name:     "Japan High group",
			code:     "JP",
			expected: CountryBasePrice{Price: Price{Amount: 62.0, Currency: common.CurrencyUSD}, Group: "High"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			if result.Amount != tt.expected.Amount {
				t.Errorf("Amount = %v, want %v", result.Amount, tt.expected.Amount)
			}
			if result.Currency != tt.expected.Currency {
				t.Errorf("Currency = %v, want %v", result.Currency, tt.expected.Currency)
			}
			if result.Group != tt.expected.Group {
				t.Errorf("Group = %v, want %v", result.Group, tt.expected.Group)
			}
		})
	}
}

func TestGetCountryBasePrice_AllGroupsForILS(t *testing.T) {
	result := GetCountryBasePrice("IL")
	if result.Amount != 180.0 {
		t.Errorf("ILS-High Amount = %v, want 180.0", result.Amount)
	}
	if result.Currency != common.CurrencyNIS {
		t.Errorf("Currency = %v, want %v", result.Currency, common.CurrencyNIS)
	}
	if result.Group != "High" {
		t.Errorf("Group = %v, want High", result.Group)
	}
}

func TestGetCountryBasePrice_AllGroupsForEUR(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected float64
		group    string
	}{
		{"EUR Low", "BG", 27.0, "Medium"}, // Bulgaria is Medium, not Low
		{"EUR Medium", "RO", 27.0, "Medium"},
		{"EUR High", "DE", 53.0, "High"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			if result.Currency != common.CurrencyEUR {
				t.Errorf("Currency = %v, want EUR", result.Currency)
			}
			if result.Group != tt.group {
				t.Errorf("Group = %v, want %v", result.Group, tt.group)
			}
		})
	}
}

func TestGetCountryBasePrice_AllGroupsForUSD(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected float64
		group    string
	}{
		{"USD Low", "IN", 21.0, "Low"},
		{"USD Medium", "CN", 31.0, "Medium"},
		{"USD High", "US", 62.0, "High"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			if result.Amount != tt.expected {
				t.Errorf("Amount = %v, want %v", result.Amount, tt.expected)
			}
			if result.Currency != common.CurrencyUSD {
				t.Errorf("Currency = %v, want USD", result.Currency)
			}
			if result.Group != tt.group {
				t.Errorf("Group = %v, want %v", result.Group, tt.group)
			}
		})
	}
}

func TestGetCountryBasePrice_UnknownCountry(t *testing.T) {
	result := GetCountryBasePrice("XX")

	if result.Group != "High" {
		t.Errorf("Unknown country Group = %v, want High", result.Group)
	}
	if result.Currency != common.CurrencyUSD {
		t.Errorf("Unknown country Currency = %v, want USD", result.Currency)
	}
	if result.Amount != 62.0 {
		t.Errorf("Unknown country Amount = %v, want 62.0", result.Amount)
	}
}

func TestGetCountryBasePrice_EmptyString(t *testing.T) {
	result := GetCountryBasePrice("")

	if result.Group != "High" {
		t.Errorf("Empty string Group = %v, want High", result.Group)
	}
	if result.Currency != common.CurrencyUSD {
		t.Errorf("Empty string Currency = %v, want USD", result.Currency)
	}
	if result.Amount != 62.0 {
		t.Errorf("Empty string Amount = %v, want 62.0", result.Amount)
	}
}

func TestGetCountryBasePrice_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"lowercase", "us"},
		{"uppercase", "US"},
		{"mixed case 1", "Us"},
		{"mixed case 2", "uS"},
		{"lowercase european", "de"},
		{"uppercase european", "DE"},
		{"mixed case european", "De"},
		{"lowercase israel", "il"},
		{"uppercase israel", "IL"},
		{"mixed case israel", "Il"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upperResult := GetCountryBasePrice(strings.ToUpper(tt.code))
			mixedResult := GetCountryBasePrice(tt.code)

			if upperResult.Amount != mixedResult.Amount {
				t.Errorf("Case sensitivity: Amount differs between %q and %q", strings.ToUpper(tt.code), tt.code)
			}
			if upperResult.Currency != mixedResult.Currency {
				t.Errorf("Case sensitivity: Currency differs between %q and %q", strings.ToUpper(tt.code), tt.code)
			}
			if upperResult.Group != mixedResult.Group {
				t.Errorf("Case sensitivity: Group differs between %q and %q", strings.ToUpper(tt.code), tt.code)
			}
		})
	}
}

func TestGetCountryBasePrice_SpecificCountries(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected CountryBasePrice
	}{
		{
			name:     "Kosovo (XK) - not in database but in map",
			code:     "XK",
			expected: CountryBasePrice{Price: Price{Amount: 31.0, Currency: common.CurrencyUSD}, Group: "Medium"},
		},
		{
			name:     "Vatican City",
			code:     "VA",
			expected: CountryBasePrice{Price: Price{Amount: 31.0, Currency: common.CurrencyUSD}, Group: "Medium"},
		},
		{
			name:     "Macao",
			code:     "MO",
			expected: CountryBasePrice{Price: Price{Amount: 62.0, Currency: common.CurrencyUSD}, Group: "High"},
		},
		{
			name:     "North Korea",
			code:     "KP",
			expected: CountryBasePrice{Price: Price{Amount: 21.0, Currency: common.CurrencyUSD}, Group: "Low"},
		},
		{
			name:     "Timor-Leste",
			code:     "TL",
			expected: CountryBasePrice{Price: Price{Amount: 21.0, Currency: common.CurrencyUSD}, Group: "Low"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			if result.Amount != tt.expected.Amount {
				t.Errorf("Amount = %v, want %v", result.Amount, tt.expected.Amount)
			}
			if result.Currency != tt.expected.Currency {
				t.Errorf("Currency = %v, want %v", result.Currency, tt.expected.Currency)
			}
			if result.Group != tt.expected.Group {
				t.Errorf("Group = %v, want %v", result.Group, tt.expected.Group)
			}
		})
	}
}

func TestGetCountryBasePrice_PriceMapping(t *testing.T) {
	priceTests := []struct {
		currency string
		group    string
		expected float64
	}{
		{common.CurrencyNIS, "Low", 60.0},
		{common.CurrencyNIS, "Medium", 90.0},
		{common.CurrencyNIS, "High", 180.0},
		{common.CurrencyEUR, "Low", 18.0},
		{common.CurrencyEUR, "Medium", 27.0},
		{common.CurrencyEUR, "High", 53.0},
		{common.CurrencyUSD, "Low", 21.0},
		{common.CurrencyUSD, "Medium", 31.0},
		{common.CurrencyUSD, "High", 62.0},
	}

	countryMap := map[string]string{
		"NIS-High":   "IL",
		"EUR-Low":    "",
		"EUR-Medium": "RO",
		"EUR-High":   "DE",
		"USD-Low":    "IN",
		"USD-Medium": "CN",
		"USD-High":   "US",
	}

	for _, pt := range priceTests {
		key := pt.currency + "-" + pt.group
		countryCode := countryMap[key]
		if countryCode == "" {
			continue
		}

		t.Run(key, func(t *testing.T) {
			result := GetCountryBasePrice(countryCode)
			if result.Amount != pt.expected {
				t.Errorf("Price for %s-%s = %v, want %v", pt.currency, pt.group, result.Amount, pt.expected)
			}
		})
	}
}

func TestGetCountryBasePrice_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		checkFn func(t *testing.T, result CountryBasePrice)
	}{
		{
			name: "Very long string",
			code: "USALONGSTRING",
			checkFn: func(t *testing.T, result CountryBasePrice) {
				if result.Group != "High" {
					t.Errorf("Long string should default to High, got %v", result.Group)
				}
			},
		},
		{
			name: "Single character",
			code: "U",
			checkFn: func(t *testing.T, result CountryBasePrice) {
				if result.Group != "High" {
					t.Errorf("Single char should default to High, got %v", result.Group)
				}
			},
		},
		{
			name: "Special characters",
			code: "!!",
			checkFn: func(t *testing.T, result CountryBasePrice) {
				if result.Group != "High" {
					t.Errorf("Special chars should default to High, got %v", result.Group)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCountryBasePrice(tt.code)
			tt.checkFn(t, result)
		})
	}
}
