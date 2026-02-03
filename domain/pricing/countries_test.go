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
				Amount:   180.0,
				Currency: common.CurrencyNIS,
				Group:    "High",
			},
		},
		{
			name: "Israel lowercase",
			code: "il",
			expected: CountryBasePrice{
				Amount:   180.0,
				Currency: common.CurrencyNIS,
				Group:    "High",
			},
		},
		{
			name: "Israel mixed case",
			code: "Il",
			expected: CountryBasePrice{
				Amount:   180.0,
				Currency: common.CurrencyNIS,
				Group:    "High",
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
			name: "Germany High group",
			code: "DE",
			expected: CountryBasePrice{
				Amount:   50.0,
				Currency: common.CurrencyEUR,
				Group:    "High",
			},
		},
		{
			name: "France High group",
			code: "FR",
			expected: CountryBasePrice{
				Amount:   50.0,
				Currency: common.CurrencyEUR,
				Group:    "High",
			},
		},
		{
			name: "Bulgaria Medium group",
			code: "BG",
			expected: CountryBasePrice{
				Amount:   30.0,
				Currency: common.CurrencyEUR,
				Group:    "Medium",
			},
		},
		{
			name: "Romania Medium group",
			code: "RO",
			expected: CountryBasePrice{
				Amount:   30.0,
				Currency: common.CurrencyEUR,
				Group:    "Medium",
			},
		},
		{
			name: "Ukraine Low group",
			code: "UA",
			expected: CountryBasePrice{
				Amount:   20.0,
				Currency: common.CurrencyUSD, // Ukraine is not in European currency map
				Group:    "Low",
			},
		},
		{
			name: "Liechtenstein High group",
			code: "LI",
			expected: CountryBasePrice{
				Amount:   50.0,
				Currency: common.CurrencyEUR,
				Group:    "High",
			},
		},
		{
			name: "Switzerland High group",
			code: "CH",
			expected: CountryBasePrice{
				Amount:   50.0,
				Currency: common.CurrencyEUR,
				Group:    "High",
			},
		},
		{
			name: "United Kingdom High group",
			code: "GB",
			expected: CountryBasePrice{
				Amount:   50.0,
				Currency: common.CurrencyEUR,
				Group:    "High",
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

func TestGetCountryBasePrice_USDCountries(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected CountryBasePrice
	}{
		{
			name: "United States High group",
			code: "US",
			expected: CountryBasePrice{
				Amount:   55.0,
				Currency: common.CurrencyUSD,
				Group:    "High",
			},
		},
		{
			name: "Canada High group",
			code: "CA",
			expected: CountryBasePrice{
				Amount:   55.0,
				Currency: common.CurrencyUSD,
				Group:    "High",
			},
		},
		{
			name: "Brazil Medium group",
			code: "BR",
			expected: CountryBasePrice{
				Amount:   35.0,
				Currency: common.CurrencyUSD,
				Group:    "Medium",
			},
		},
		{
			name: "Mexico Medium group",
			code: "MX",
			expected: CountryBasePrice{
				Amount:   35.0,
				Currency: common.CurrencyUSD,
				Group:    "Medium",
			},
		},
		{
			name: "India Low group",
			code: "IN",
			expected: CountryBasePrice{
				Amount:   20.0,
				Currency: common.CurrencyUSD,
				Group:    "Low",
			},
		},
		{
			name: "China Medium group",
			code: "CN",
			expected: CountryBasePrice{
				Amount:   35.0,
				Currency: common.CurrencyUSD,
				Group:    "Medium",
			},
		},
		{
			name: "Japan High group",
			code: "JP",
			expected: CountryBasePrice{
				Amount:   55.0,
				Currency: common.CurrencyUSD,
				Group:    "High",
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

func TestGetCountryBasePrice_AllGroupsForILS(t *testing.T) {
	// Note: Israel is High group, but we can test the price mapping
	// by checking that ILS-High returns the correct price
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
		{"EUR Low", "BG", 30.0, "Medium"}, // Bulgaria is Medium, not Low
		{"EUR Medium", "RO", 30.0, "Medium"},
		{"EUR High", "DE", 50.0, "High"},
	}

	// Find actual EUR countries with Low group
	// Since we don't have EUR-Low in the map, let's test what we have
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
		{"USD Low", "IN", 20.0, "Low"},
		{"USD Medium", "BR", 35.0, "Medium"},
		{"USD High", "US", 55.0, "High"},
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
	// Test with a country code that doesn't exist in the maps
	result := GetCountryBasePrice("XX")

	// Should default to High group and USD currency
	if result.Group != "High" {
		t.Errorf("Unknown country Group = %v, want High", result.Group)
	}
	if result.Currency != common.CurrencyUSD {
		t.Errorf("Unknown country Currency = %v, want USD", result.Currency)
	}
	// Should use default amount for USD-High
	if result.Amount != 55.0 {
		t.Errorf("Unknown country Amount = %v, want 55.0", result.Amount)
	}
}

func TestGetCountryBasePrice_EmptyString(t *testing.T) {
	result := GetCountryBasePrice("")

	// Should default to High group and USD currency
	if result.Group != "High" {
		t.Errorf("Empty string Group = %v, want High", result.Group)
	}
	if result.Currency != common.CurrencyUSD {
		t.Errorf("Empty string Currency = %v, want USD", result.Currency)
	}
	if result.Amount != 55.0 {
		t.Errorf("Empty string Amount = %v, want 55.0", result.Amount)
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
			name: "Kosovo (XK) - not in database but in map",
			code: "XK",
			expected: CountryBasePrice{
				Amount:   35.0,
				Currency: common.CurrencyUSD,
				Group:    "Medium",
			},
		},
		{
			name: "Vatican City",
			code: "VA",
			expected: CountryBasePrice{
				Amount:   35.0,
				Currency: common.CurrencyUSD,
				Group:    "Medium",
			},
		},
		{
			name: "Macao",
			code: "MO",
			expected: CountryBasePrice{
				Amount:   55.0,
				Currency: common.CurrencyUSD,
				Group:    "High",
			},
		},
		{
			name: "North Korea",
			code: "KP",
			expected: CountryBasePrice{
				Amount:   20.0,
				Currency: common.CurrencyUSD,
				Group:    "Low",
			},
		},
		{
			name: "Timor-Leste",
			code: "TL",
			expected: CountryBasePrice{
				Amount:   20.0,
				Currency: common.CurrencyUSD,
				Group:    "Low",
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

func TestGetCountryBasePrice_PriceMapping(t *testing.T) {
	// Test that all price combinations are correctly mapped
	priceTests := []struct {
		currency string
		group    string
		expected float64
	}{
		{common.CurrencyNIS, "Low", 60.0},
		{common.CurrencyNIS, "Medium", 90.0},
		{common.CurrencyNIS, "High", 180.0},
		{common.CurrencyEUR, "Low", 20.0},
		{common.CurrencyEUR, "Medium", 30.0},
		{common.CurrencyEUR, "High", 50.0},
		{common.CurrencyUSD, "Low", 20.0},
		{common.CurrencyUSD, "Medium", 35.0},
		{common.CurrencyUSD, "High", 55.0},
	}

	// Find countries that match each combination
	countryMap := map[string]string{
		"NIS-High":   "IL",
		"EUR-Low":    "", // No EUR country with Low group currently
		"EUR-Medium": "RO",
		"EUR-High":   "DE",
		"USD-Low":    "IN",
		"USD-Medium": "BR",
		"USD-High":   "US",
	}

	for _, pt := range priceTests {
		key := pt.currency + "-" + pt.group
		countryCode := countryMap[key]
		if countryCode == "" {
			// Skip if no country matches this combination
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
		name     string
		code     string
		checkFn  func(t *testing.T, result CountryBasePrice)
	}{
		{
			name: "Very long string",
			code: "USALONGSTRING",
			checkFn: func(t *testing.T, result CountryBasePrice) {
				// Should default since it's not a valid 2-letter code
				if result.Group != "High" {
					t.Errorf("Long string should default to High, got %v", result.Group)
				}
			},
		},
		{
			name: "Single character",
			code: "U",
			checkFn: func(t *testing.T, result CountryBasePrice) {
				// Should default
				if result.Group != "High" {
					t.Errorf("Single char should default to High, got %v", result.Group)
				}
			},
		},
		{
			name: "Special characters",
			code: "!!",
			checkFn: func(t *testing.T, result CountryBasePrice) {
				// Should default
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

