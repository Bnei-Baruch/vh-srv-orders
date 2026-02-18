package billing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func TestGenerateDryRunMuhlafim_EmptyTokenMap(t *testing.T) {
	result := generateDryRunMuhlafim(map[int]string{})
	assert.Empty(t, result)
}

func TestGenerateDryRunMuhlafim_SkipsEmptyTokens(t *testing.T) {
	tokenMap := map[int]string{
		1: "",
		2: "",
	}
	result := generateDryRunMuhlafim(tokenMap)
	assert.Empty(t, result)
}

func TestGenerateDryRunMuhlafim_MatchRate(t *testing.T) {
	// With 10000 orders, expect ~0.5% match rate (50 matches, give or take)
	tokenMap := make(map[int]string, 10000)
	for i := 1; i <= 10000; i++ {
		tokenMap[i] = fmt.Sprintf("token_%d", i)
	}

	result := generateDryRunMuhlafim(tokenMap)

	// 0.5% of 10000 = 50, allow reasonable tolerance
	assert.Greater(t, len(result), 20, "should have at least some matches")
	assert.Less(t, len(result), 100, "should not have too many matches")
}

func TestGenerateDryRunMuhlafim_NewCardVsFlagDistribution(t *testing.T) {
	// With enough orders, verify 80% new card vs 20% flag.
	// The split is exact: h values {0,1,2,3} -> new card (80%), h == 4 -> flag (20%).
	tokenMap := make(map[int]string, 100000)
	for i := 1; i <= 100000; i++ {
		tokenMap[i] = fmt.Sprintf("token_%d", i)
	}

	result := generateDryRunMuhlafim(tokenMap)

	newCards := 0
	flagged := 0
	for _, entry := range result {
		if entry.NewCardNumber != "" {
			newCards++
		} else {
			flagged++
		}
	}

	total := newCards + flagged
	assert.Greater(t, total, 0, "should have matches")
	assert.Greater(t, newCards, 0, "should have new card entries")
	assert.Greater(t, flagged, 0, "should have flagged entries")

	newCardPct := float64(newCards) / float64(total) * 100
	assert.Greater(t, newCardPct, 70.0, "new card rate should be ~80%%")
	assert.Less(t, newCardPct, 90.0, "new card rate should be ~80%%")
}

func TestGenerateDryRunMuhlafim_Deterministic(t *testing.T) {
	tokenMap := map[int]string{
		100: "tok_100",
		200: "tok_200",
		300: "tok_300",
	}

	result1 := generateDryRunMuhlafim(tokenMap)
	result2 := generateDryRunMuhlafim(tokenMap)

	assert.Equal(t, result1, result2, "same input should produce same output")
}

func TestGenerateDryRunMuhlafim_FlaggedEntriesHaveValidActions(t *testing.T) {
	tokenMap := make(map[int]string, 100000)
	for i := 1; i <= 100000; i++ {
		tokenMap[i] = fmt.Sprintf("token_%d", i)
	}

	result := generateDryRunMuhlafim(tokenMap)

	for _, entry := range result {
		if entry.NewCardNumber != "" {
			continue // new card entries are fine
		}
		// Flagged entries should have a non-empty action description
		assert.NotEmpty(t, entry.ActionDescription, "flagged entries must have an action description")
	}
}

func TestDryRunMuhFlag_AllFlagsCovered(t *testing.T) {
	// Verify that dryRunMuhFlag can produce all expected flag values
	flags := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		flags[dryRunMuhFlag(i)] = true
	}

	assert.True(t, flags[common.OrderFlagMuhHiyuvNiklat], "should produce muh_hiyuv_niklat")
	assert.True(t, flags[common.OrderFlagMuhNidha], "should produce muh_nidha")
	assert.True(t, flags[common.OrderFlagMuhBitul], "should produce muh_bitul")
	assert.True(t, flags[common.OrderFlagMuhLotakin], "should produce muh_lotakin")
	assert.True(t, flags[common.OrderFlagMuhAher], "should produce muh_aher")
}
