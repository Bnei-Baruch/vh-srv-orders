package priority

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSumGroup_SumsAcrossDistinctCustomers(t *testing.T) {
	result := &ContributionsBatchResult{
		ByCustomer: map[string]map[string]float64{
			"CUST001": {"NIS": 100},
			"CUST002": {"NIS": 200},
		},
		CustNamesByEmail: map[string][]string{
			"a@x.com": {"CUST001"},
			"b@x.com": {"CUST002"},
		},
	}

	assert.Equal(t, map[string]float64{"NIS": 300}, result.SumGroup([]string{"a@x.com", "b@x.com"}))
}

func TestSumGroup_DedupsSharedCustomer(t *testing.T) {
	result := &ContributionsBatchResult{
		ByCustomer: map[string]map[string]float64{
			"CUST001": {"NIS": 100},
		},
		CustNamesByEmail: map[string][]string{
			"a@x.com": {"CUST001"},
			"b@x.com": {"CUST001"}, // spouse alias, same Priority customer
		},
	}

	assert.Equal(t, map[string]float64{"NIS": 100}, result.SumGroup([]string{"a@x.com", "b@x.com"}))
}

func TestSumGroup_EmailNotFound_ReturnsEmpty(t *testing.T) {
	result := &ContributionsBatchResult{
		ByCustomer:       map[string]map[string]float64{},
		CustNamesByEmail: map[string][]string{},
	}

	assert.Empty(t, result.SumGroup([]string{"unknown@x.com"}))
}

func TestSumGroup_EmptyGroup_ReturnsEmpty(t *testing.T) {
	result := &ContributionsBatchResult{
		ByCustomer: map[string]map[string]float64{"CUST001": {"NIS": 100}},
		CustNamesByEmail: map[string][]string{
			"a@x.com": {"CUST001"},
		},
	}

	assert.Empty(t, result.SumGroup(nil))
}

func TestSumGroup_LookupIsCaseInsensitive(t *testing.T) {
	result := &ContributionsBatchResult{
		ByCustomer: map[string]map[string]float64{"CUST001": {"NIS": 100}},
		CustNamesByEmail: map[string][]string{
			"a@x.com": {"CUST001"}, // stored lower-cased, as GetLastContributionsBatch does
		},
	}

	assert.Equal(t, map[string]float64{"NIS": 100}, result.SumGroup([]string{"A@X.com"}))
}

func TestSumGroup_MultipleCurrenciesPerCustomer(t *testing.T) {
	result := &ContributionsBatchResult{
		ByCustomer: map[string]map[string]float64{
			"CUST001": {"NIS": 100, "USD": 50},
		},
		CustNamesByEmail: map[string][]string{
			"a@x.com": {"CUST001"},
		},
	}

	assert.Equal(t, map[string]float64{"NIS": 100, "USD": 50}, result.SumGroup([]string{"a@x.com"}))
}
