package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSumsMatch_EqualMaps(t *testing.T) {
	a := map[string]float64{"NIS": 100, "USD": 50}
	b := map[string]float64{"NIS": 100, "USD": 50}
	assert.True(t, sumsMatch(a, b))
}

func TestSumsMatch_BothEmpty(t *testing.T) {
	assert.True(t, sumsMatch(map[string]float64{}, map[string]float64{}))
}

func TestSumsMatch_WithinEpsilon(t *testing.T) {
	a := map[string]float64{"NIS": 100.001}
	b := map[string]float64{"NIS": 100.005}
	assert.True(t, sumsMatch(a, b))
}

func TestSumsMatch_ExceedsEpsilon(t *testing.T) {
	a := map[string]float64{"NIS": 100}
	b := map[string]float64{"NIS": 100.02}
	assert.False(t, sumsMatch(a, b))
}

func TestSumsMatch_ExtraCurrencyOnlyInA(t *testing.T) {
	a := map[string]float64{"NIS": 100, "USD": 50}
	b := map[string]float64{"NIS": 100}
	assert.False(t, sumsMatch(a, b))
}

func TestSumsMatch_ExtraCurrencyOnlyInB(t *testing.T) {
	a := map[string]float64{"NIS": 100}
	b := map[string]float64{"NIS": 100, "USD": 50}
	assert.False(t, sumsMatch(a, b))
}

func TestSumsMatch_ExtraCurrencyWithinEpsilonOfZero(t *testing.T) {
	// A currency present on only one side but effectively zero shouldn't count as a mismatch.
	a := map[string]float64{"NIS": 100, "USD": 0.001}
	b := map[string]float64{"NIS": 100}
	assert.True(t, sumsMatch(a, b))
}
