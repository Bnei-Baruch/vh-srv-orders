package billing

import (
	"time"
)

// BillingPeriod holds configuration for billing operations
type BillingPeriod struct {
	Month int
	Year  int
}

// NewBillingPeriod creates a new billing period with current month/year
func NewBillingPeriod() *BillingPeriod {
	now := time.Now()
	return &BillingPeriod{
		Month: int(now.Month()),
		Year:  now.Year(),
	}
}

// NewBillingPeriodWithDate creates a new billing period with specified month/year
func NewBillingPeriodWithDate(month, year int) *BillingPeriod {
	return &BillingPeriod{
		Month: month,
		Year:  year,
	}
}

// GetStartOfMonth returns the start of the billing month (first day at 00:00:00)
func (p *BillingPeriod) GetStartOfMonth() time.Time {
	return time.Date(p.Year, time.Month(p.Month), 1, 0, 0, 0, 0, time.UTC)
}

// GetEndOfMonth returns the end of the billing month (last day at 23:59:59)
func (p *BillingPeriod) GetEndOfMonth() time.Time {
	// Get first day of next month, then subtract 1 second
	nextMonth := time.Date(p.Year, time.Month(p.Month)+1, 1, 0, 0, 0, 0, time.UTC)
	return nextMonth.Add(-time.Second)
}

// GetLastDayOfLastMonth returns the last day of the previous month at 23:59:59
func (p *BillingPeriod) GetLastDayOfLastMonth() time.Time {
	// Get first day of current month, then subtract 1 second to get 23:59:59 of previous month
	firstDay := p.GetStartOfMonth()
	return firstDay.Add(-time.Second)
}

// GetStartOfLastMonth returns the start of the previous month
func (p *BillingPeriod) GetStartOfLastMonth() time.Time {
	firstDay := p.GetStartOfMonth()
	return firstDay.AddDate(0, -1, 0)
}

// GetEndOfLastMonth returns the end of the previous month
func (p *BillingPeriod) GetEndOfLastMonth() time.Time {
	firstDay := p.GetStartOfMonth()
	return firstDay.Add(-time.Second)
}

// GetMuhlafimStartDate returns the start date for muhlafim processing
// Goes back to the 1st of 2 months ago to catch all card status changes
func (p *BillingPeriod) GetMuhlafimStartDate() time.Time {
	// Go back 2 months from the current billing month
	return p.GetStartOfMonth().AddDate(0, -2, 0)
}

func (p *BillingPeriod) GetMuhlafimEndDate() time.Time {
	now := time.Now().UTC()
	if p.Year == now.Year() && p.Month == int(now.Month()) {
		return now
	}
	return time.Date(p.Year, time.Month(p.Month), 20, 0, 0, 0, 0, time.UTC)
}
