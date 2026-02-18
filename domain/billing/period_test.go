package billing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewBillingPeriod(t *testing.T) {
	config := NewBillingPeriod()
	now := time.Now()

	assert.Equal(t, int(now.Month()), config.Month)
	assert.Equal(t, now.Year(), config.Year)
}

func TestNewBillingPeriodWithDate(t *testing.T) {
	month := 6
	year := 2024
	config := NewBillingPeriodWithDate(month, year)

	assert.Equal(t, month, config.Month)
	assert.Equal(t, year, config.Year)
}

func TestGetStartOfMonth(t *testing.T) {
	config := NewBillingPeriodWithDate(6, 2024)
	start := config.GetStartOfMonth()

	expected := time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, start.Equal(expected), "Expected %v, got %v", expected, start)
}

func TestGetEndOfMonth(t *testing.T) {
	tests := []struct {
		name     string
		month    int
		year     int
		expected time.Time
	}{
		{
			name:     "January 2024 (31 days)",
			month:    1,
			year:     2024,
			expected: time.Date(2024, time.January, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "February 2024 (leap year, 29 days)",
			month:    2,
			year:     2024,
			expected: time.Date(2024, time.February, 29, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "February 2023 (non-leap year, 28 days)",
			month:    2,
			year:     2023,
			expected: time.Date(2023, time.February, 28, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "April 2024 (30 days)",
			month:    4,
			year:     2024,
			expected: time.Date(2024, time.April, 30, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "December 2024 (31 days)",
			month:    12,
			year:     2024,
			expected: time.Date(2024, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewBillingPeriodWithDate(tt.month, tt.year)
			end := config.GetEndOfMonth()
			assert.True(t, end.Equal(tt.expected), "Expected %v, got %v", tt.expected, end)
		})
	}
}

func TestGetLastDayOfLastMonth(t *testing.T) {
	tests := []struct {
		name     string
		month    int
		year     int
		expected time.Time
	}{
		{
			name:     "January 2024 (previous month is December 2023)",
			month:    1,
			year:     2024,
			expected: time.Date(2023, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "March 2024 (previous month is February 2024, leap year)",
			month:    3,
			year:     2024,
			expected: time.Date(2024, time.February, 29, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "January 2023 (previous month is December 2022)",
			month:    1,
			year:     2023,
			expected: time.Date(2022, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewBillingPeriodWithDate(tt.month, tt.year)
			lastDay := config.GetLastDayOfLastMonth()
			assert.True(t, lastDay.Equal(tt.expected), "Expected %v, got %v", tt.expected, lastDay)
		})
	}
}

func TestGetStartOfLastMonth(t *testing.T) {
	tests := []struct {
		name     string
		month    int
		year     int
		expected time.Time
	}{
		{
			name:     "January 2024 (previous month is December 2023)",
			month:    1,
			year:     2024,
			expected: time.Date(2023, time.December, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "March 2024 (previous month is February 2024)",
			month:    3,
			year:     2024,
			expected: time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewBillingPeriodWithDate(tt.month, tt.year)
			start := config.GetStartOfLastMonth()
			assert.True(t, start.Equal(tt.expected), "Expected %v, got %v", tt.expected, start)
		})
	}
}

func TestGetEndOfLastMonth(t *testing.T) {
	config := NewBillingPeriodWithDate(6, 2024)
	end := config.GetEndOfLastMonth()

	// Should be one second before the start of current month
	startOfMonth := config.GetStartOfMonth()
	expected := startOfMonth.Add(-time.Second)

	assert.True(t, end.Equal(expected), "Expected %v, got %v", expected, end)
}

func TestGetMuhlafimStartDate(t *testing.T) {
	tests := []struct {
		name     string
		month    int
		year     int
		expected time.Time
	}{
		{
			name:     "June 2024 -> 1st of April 2024",
			month:    6,
			year:     2024,
			expected: time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "January 2024 -> 1st of November 2023",
			month:    1,
			year:     2024,
			expected: time.Date(2023, time.November, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "February 2024 -> 1st of December 2023",
			month:    2,
			year:     2024,
			expected: time.Date(2023, time.December, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "March 2024 -> 1st of January 2024",
			month:    3,
			year:     2024,
			expected: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewBillingPeriodWithDate(tt.month, tt.year)
			start := p.GetMuhlafimStartDate()
			assert.True(t, start.Equal(tt.expected), "Expected %v, got %v", tt.expected, start)
		})
	}
}

func TestGetMuhlafimEndDate(t *testing.T) {
	t.Run("past month returns 20th of billing month", func(t *testing.T) {
		// Use a date far in the past so it won't be "current" month
		p := NewBillingPeriodWithDate(6, 2020)
		end := p.GetMuhlafimEndDate()

		expected := time.Date(2020, time.June, 20, 0, 0, 0, 0, time.UTC)
		assert.True(t, end.Equal(expected), "Expected %v, got %v", expected, end)
	})

	t.Run("current month returns now", func(t *testing.T) {
		now := time.Now().UTC()
		p := NewBillingPeriodWithDate(int(now.Month()), now.Year())
		end := p.GetMuhlafimEndDate()

		// end should be approximately now (within a few seconds)
		diff := now.Sub(end)
		if diff < 0 {
			diff = -diff
		}
		assert.Less(t, diff, 5*time.Second, "End date should be approximately now, got diff %v", diff)
	})
}
