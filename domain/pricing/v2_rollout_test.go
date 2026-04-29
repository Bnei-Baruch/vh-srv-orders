package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestV2Eligible_IsraelIsEligible(t *testing.T) {
	assert.True(t, V2Eligible("IL"))
}

func TestV2Eligible_CaseInsensitive(t *testing.T) {
	assert.True(t, V2Eligible("il"))
	assert.True(t, V2Eligible("Il"))
	assert.False(t, V2Eligible("us"))
	assert.False(t, V2Eligible("Us"))
}

func TestV2Eligible_USExcluded(t *testing.T) {
	assert.False(t, V2Eligible("US"))
}

func TestV2Eligible_UKExcluded(t *testing.T) {
	assert.False(t, V2Eligible("GB"))
}

func TestV2Eligible_EUCountriesExcluded(t *testing.T) {
	euCountries := []string{
		"AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR",
		"DE", "GR", "HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL",
		"PL", "PT", "RO", "SK", "SI", "ES", "SE",
	}
	for _, code := range euCountries {
		assert.False(t, V2Eligible(code), "EU country %s should be excluded", code)
	}
}

func TestV2Eligible_TurkeyRussiaUkraineExcluded(t *testing.T) {
	assert.False(t, V2Eligible("TR"))
	assert.False(t, V2Eligible("RU"))
	assert.False(t, V2Eligible("UA"))
}

func TestV2Eligible_NorwaySwitzerlandExcluded(t *testing.T) {
	assert.False(t, V2Eligible("NO"))
	assert.False(t, V2Eligible("CH"))
}

func TestV2Eligible_CanadaExcluded(t *testing.T) {
	assert.False(t, V2Eligible("CA"))
}

func TestV2Eligible_LatinAmericaAndCaribbeanExcluded(t *testing.T) {
	countries := []string{
		"AR", "BO", "BR", "BZ", "CL", "CO", "CR", "CU", "DO", "EC",
		"GT", "GY", "HN", "HT", "JM", "MX", "NI", "PA", "PE", "PY",
		"SR", "SV", "TT", "UY", "VE",
	}
	for _, code := range countries {
		assert.False(t, V2Eligible(code), "Latin America / Caribbean country %s should be excluded", code)
	}
}

func TestV2Eligible_OtherCountriesEligible(t *testing.T) {
	eligible := []string{"IN", "JP", "AU", "ZA", "KE", "TH", "CN"}
	for _, code := range eligible {
		assert.True(t, V2Eligible(code), "country %s should be v2 eligible", code)
	}
}

func TestV2Eligible_UnknownCountryIsEligible(t *testing.T) {
	assert.True(t, V2Eligible("XX"))
}

func TestV2Eligible_MissingCountryIsIneligible(t *testing.T) {
	assert.False(t, V2Eligible(""))
}
