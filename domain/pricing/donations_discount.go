package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

// ErrDonationFetch is returned when a real API error (not "customer not found") occurs
// while fetching Priority ERP donation data. The price must not be used when this occurs.
var ErrDonationFetch = fmt.Errorf("donation fetch error")

// DonationsDiscountProperties holds all data specific to the donations discount type.
//
// Donation amounts fetched from Priority ERP are deliberately excluded — they are
// used only during calculation and must never be persisted, logged, or returned.
//
// NOTE: Emails are deduplicated across primary and spouse before fetching, but if two
// different users (not spouses) happen to share an email address, their donations may
// be double-counted. This is a known limitation and is not validated here.
type DonationsDiscountProperties struct {
	// Email collection counts
	PrimaryEmailCount int `json:"primary_email_count"`
	SpouseEmailCount  int `json:"spouse_email_count,omitempty"`

	// Spouse identity (omitted if no spouse)
	SpouseKeycloakID   string `json:"spouse_keycloak_id,omitempty"`
	SpouseGetsDiscount bool   `json:"spouse_gets_discount,omitempty"`

	// Priority fetch metadata — DonationsFetchError and DonationsFetched removed:
	// any real API error now surfaces as a pricing error before reaching this point,
	// so donations are always successfully queried when this struct is populated.
	DonationsFetchedEmails []string `json:"donations_fetched_emails,omitempty"`
	DonationsFetchNote     string   `json:"donations_fetch_note,omitempty"`

	// Annual threshold reference (monthly base × 12, in base currency — not a donation amount)
	AnnualBase Price `json:"annual_base"`
}

// donationSums holds aggregated Priority ERP donation data.
// Used only during v2 price calculation — never stored, logged, or returned.
type donationSums struct {
	perCurrency   map[string]float64
	totalNIS      float64
	successEmails []string // emails that returned data from Priority without error
	fetchNote     string   // informational (e.g. "customer not found" for some emails)
}

// buildDonationsDiscount returns the Discount record for the donations discount type,
// a bool indicating whether the primary account receives the discounted price,
// and explain lines describing the email fetch and threshold evaluation.
//
// Threshold rule:
//   - No spouse: eligible if totalNIS >= annual (12 months of base price)
//   - With spouse: both eligible if totalNIS >= 2×annual (12 months per person); otherwise neither
func buildDonationsDiscount(
	sums donationSums,
	base CountryBasePrice,
	basePriceNIS float64,
	primaryEmailCount, spouseEmailCount int,
	spouseKeycloakID string,
) (Discount, bool, []string) {
	annualNIS := basePriceNIS * 12
	hasSpouse := spouseKeycloakID != ""

	threshold := annualNIS
	if hasSpouse {
		threshold = 2 * annualNIS
	}
	eligible := sums.totalNIS >= threshold
	primaryGetsDiscount := eligible
	spouseGetsDiscount := eligible && hasSpouse

	props := DonationsDiscountProperties{
		PrimaryEmailCount:      primaryEmailCount,
		SpouseEmailCount:       spouseEmailCount,
		SpouseKeycloakID:       spouseKeycloakID,
		SpouseGetsDiscount:     spouseGetsDiscount,
		DonationsFetchedEmails: sums.successEmails,
		DonationsFetchNote:     sums.fetchNote,
		AnnualBase:             Price{Amount: base.Amount * 12, Currency: base.Currency},
	}
	propsJSON, _ := json.Marshal(props)

	fetchStatus := "ok"
	if sums.fetchNote != "" {
		fetchStatus += ": " + sums.fetchNote
	}
	var emailLine string
	if hasSpouse {
		emailLine = fmt.Sprintf("emails: primary(%d) + spouse(%d) = %d unique → Priority ERP fetch → %s",
			primaryEmailCount, spouseEmailCount, primaryEmailCount+spouseEmailCount, fetchStatus)
	} else {
		emailLine = fmt.Sprintf("emails: primary(%d) → Priority ERP fetch → %s",
			primaryEmailCount, fetchStatus)
	}

	annualAmount := base.Amount * 12
	cur := base.Currency
	nisMarker := ""
	if cur != common.CurrencyNIS {
		nisMarker = " (→ NIS)"
	}
	discountPct := int(DonationsDiscountAmtPct)
	var thresholdLine string
	switch {
	case hasSpouse && eligible:
		thresholdLine = fmt.Sprintf("threshold: combined >= %.2f %s/yr%s → both get %d%% off",
			annualAmount*2, cur, nisMarker, discountPct)
	case hasSpouse:
		thresholdLine = fmt.Sprintf("threshold: combined < %.2f %s/yr%s → no discount",
			annualAmount*2, cur, nisMarker)
	case eligible:
		thresholdLine = fmt.Sprintf("threshold: combined >= %.2f %s/yr%s → %d%% off",
			annualAmount, cur, nisMarker, discountPct)
	default:
		thresholdLine = fmt.Sprintf("threshold: combined < %.2f %s/yr%s → no discount",
			annualAmount, cur, nisMarker)
	}

	lines := []string{
		emailLine,
		"donations: aggregate per currency → convert to NIS",
		thresholdLine,
	}

	return Discount{
		Type:       DiscountTypeDonations,
		AmountPct:  DonationsDiscountAmtPct,
		Eligible:   eligible,
		Properties: propsJSON,
	}, primaryGetsDiscount, lines
}

// deduplicateEmails merges two email slices, removing duplicates (case-insensitive).
// Order is preserved: all of a comes first, then new entries from b.
func deduplicateEmails(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, e := range append(a, b...) {
		key := strings.ToLower(e)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, e)
	}
	return result
}

// collectProfileEmails returns a deduplicated, non-empty slice of emails from the profile.
// Falls back to fallbackEmail if the profile has no usable emails.
func collectProfileEmails(profile *profiles.Profile, fallbackEmail string) []string {
	seen := make(map[string]struct{})
	var emails []string

	add := func(s *string) {
		if s == nil || *s == "" {
			return
		}
		key := strings.ToLower(*s)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		emails = append(emails, *s)
	}

	if profile != nil {
		add(profile.PrimaryEmail)
		add(profile.AlternateEmail1)
		add(profile.AlternateEmail2)
	}

	if len(emails) == 0 && fallbackEmail != "" {
		emails = append(emails, fallbackEmail)
	}

	return emails
}

// fetchDonationSums queries Priority ERP for each email and aggregates results.
// "Customer not found" responses are treated as zero donations (not an error).
// Any other API error is returned immediately — the price must not be used.
// Donation amounts in the returned struct must never be stored, logged, or returned.
func fetchDonationSums(ctx context.Context, client *priority.Client, emails []string, usdRate, eurRate float64) (donationSums, error) {
	result := donationSums{
		perCurrency: make(map[string]float64),
	}

	var notFoundNotes []string
	for _, email := range emails {
		contributions, err := client.GetLastContributions(ctx, email)
		if err != nil {
			if errors.Is(err, priority.ErrNoActiveCustomers) {
				notFoundNotes = append(notFoundNotes, email)
				continue // no Priority account for this email — treat as zero donations
			}
			return donationSums{}, fmt.Errorf("client.GetLastContributions %w: %s: %v", ErrDonationFetch, email, err)
		}
		result.successEmails = append(result.successEmails, email)
		for currency, amount := range contributions {
			result.perCurrency[currency] += amount
		}
	}

	if len(notFoundNotes) > 0 {
		result.fetchNote = fmt.Sprintf("no Priority account for emails: %s", strings.Join(notFoundNotes, ", "))
	}

	for currency, amount := range result.perCurrency {
		result.totalNIS += toNIS(amount, currency, usdRate, eurRate)
	}

	return result, nil
}
