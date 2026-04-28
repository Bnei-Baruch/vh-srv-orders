package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// Currency conversion ratios to NIS (hardcoded — update when rates change significantly).
const (
	USDToNIS = 3.1 // 1 USD = 3.1 NIS
	EURToNIS = 3.6 // 1 EUR = 3.6 NIS
)

// ErrDonationFetch is returned when a real API error (not "customer not found") occurs
// while fetching Priority ERP donation data. The price must not be used when this occurs.
var ErrDonationFetch = fmt.Errorf("donation fetch error")

// DiscountType identifies a discount by name.
type DiscountType string

const (
	DiscountTypeDonations   DiscountType = "donations"
	DonationsDiscountAmtPct              = 55.0 // percent off: final price = base * (1 - DonationsDiscountAmtPct/100)
)

// Discount is a generic discount record. Properties holds type-specific data as JSON.
type Discount struct {
	Type       DiscountType    `json:"type"`
	AmountPct  float64         `json:"amount_pct"` // percent off (e.g. 55.0 means pay 45%)
	Eligible   bool            `json:"eligible"`
	Properties json.RawMessage `json:"properties,omitempty"`
}

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

// V2PricingEvaluation is the storable record of a v2 pricing evaluation.
// It captures inputs, applied discounts, and the final price for audit and debugging.
type V2PricingEvaluation struct {
	EvaluatedAt time.Time `json:"evaluated_at"`

	AccountID   int              `json:"account_id"`
	CountryCode string           `json:"country_code"`
	CountryBase CountryBasePrice `json:"country_base"`

	Discounts  []Discount `json:"discounts"`
	FinalPrice Price      `json:"final_price"`

	// Structural audit explanation — describes the shape of the calculation
	// without any Priority donation amounts.
	Explain []string `json:"explain"`
}

// Public returns a copy of the evaluation with admin-only fields stripped.
// Removes Explain and each Discount's Properties, retaining only type, amount_pct, and eligible.
func (e *V2PricingEvaluation) Public() *V2PricingEvaluation {
	pub := *e
	pub.Explain = nil
	pub.Discounts = make([]Discount, len(e.Discounts))
	for i, d := range e.Discounts {
		pub.Discounts[i] = Discount{
			Type:      d.Type,
			AmountPct: d.AmountPct,
			Eligible:  d.Eligible,
		}
	}
	return &pub
}

// donationSums holds aggregated Priority ERP donation data.
// Used only during v2 price calculation — never stored, logged, or returned.
type donationSums struct {
	perCurrency   map[string]float64
	totalNIS      float64
	successEmails []string // emails that returned data from Priority without error
	fetchNote     string   // informational (e.g. "customer not found" for some emails)
}

// EvaluateV2Price computes the full v2 pricing evaluation for a member account.
//
// Donation amounts fetched from Priority ERP exist only in local variables
// within this function. They must never be stored, logged, or returned.
func EvaluateV2Price(
	ctx context.Context,
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	primaryAccountID int,
	primaryKeycloakID string,
	primaryEmail string,
	country string,
) (*V2PricingEvaluation, error) {
	log := utils.LogFor(ctx)
	log.Info("EvaluateV2Price: start",
		slog.Int("account_id", primaryAccountID),
		slog.String("keycloak_id", primaryKeycloakID),
		slog.String("country", country),
	)

	base := GetCountryBasePrice(country)
	inputs := &V2PricingEvaluation{
		EvaluatedAt: time.Now().UTC(),
		AccountID:   primaryAccountID,
		CountryCode: country,
		CountryBase: base,
	}

	// Fetch primary profile to get all emails and spouse info.
	// Soft-fail on not found: fall back to account email.
	profile, err := profileService.GetProfileByKeycloakID(ctx, primaryKeycloakID)
	if err != nil && !errors.Is(err, profiles.ErrNotFound) {
		return nil, fmt.Errorf("profileService.GetProfileByKeycloakID (primary) %s: %w", primaryKeycloakID, err)
	}
	primaryEmails := collectProfileEmails(profile, primaryEmail)
	primaryEmailCount := len(primaryEmails)
	log.Info("EvaluateV2Price: primary profile done",
		slog.Bool("profile_found", profile != nil),
		slog.Int("email_count", primaryEmailCount),
	)

	// Collect spouse emails if a spouse is linked.
	// Profile service is the source of truth for emails — no DB lookup needed.
	// A spouse may have Priority donations without a VH account.
	var (
		spouseEmails     []string
		spouseKeycloakID string
		spouseEmailCount int
	)
	if profile != nil && profile.SpouseKeycloakID != nil && *profile.SpouseKeycloakID != "" {
		spouseKeycloakID = *profile.SpouseKeycloakID
		log.Info("EvaluateV2Price: fetching spouse profile", slog.String("spouse_keycloak_id", spouseKeycloakID))
		spouseProfile, err := profileService.GetProfileByKeycloakID(ctx, spouseKeycloakID)
		if err != nil && !errors.Is(err, profiles.ErrNotFound) {
			return nil, fmt.Errorf("profileService.GetProfileByKeycloakID (spouse) %s: %w", spouseKeycloakID, err)
		}
		spouseEmails = collectProfileEmails(spouseProfile, "")
		spouseEmailCount = len(spouseEmails)
		log.Info("EvaluateV2Price: spouse profile done",
			slog.Bool("profile_found", spouseProfile != nil),
			slog.Int("email_count", spouseEmailCount),
		)
	}

	// Fetch donations once with all emails deduplicated across primary and spouse.
	// Amounts stay in local variable only — never persisted or returned.
	emails := deduplicateEmails(primaryEmails, spouseEmails)
	log.Info("EvaluateV2Price: fetching donations", slog.Int("email_count", len(emails)))
	sums, err := fetchDonationSums(ctx, priorityClient, emails, USDToNIS, EURToNIS)
	if err != nil {
		return nil, fmt.Errorf("fetchDonationSums: %w", err)
	}
	log.Info("EvaluateV2Price: fetched donations",
		slog.Int("success_email_count", len(sums.successEmails)),
		slog.String("note", sums.fetchNote),
	)

	basePriceNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS)

	discount, primaryGetsDiscount := buildDonationsDiscount(
		sums, base, basePriceNIS,
		primaryEmailCount, spouseEmailCount,
		spouseKeycloakID,
	)
	inputs.Discounts = []Discount{discount}

	if primaryGetsDiscount {
		inputs.FinalPrice = Price{Amount: math.Round(base.Amount*(1-DonationsDiscountAmtPct/100)*100) / 100, Currency: base.Currency}
	} else {
		inputs.FinalPrice = Price{Amount: base.Amount, Currency: base.Currency}

		// A log marker for edge case where one is better off alone than with a spouse.
		// TODO: revisit this logic when we have a better way to distinguish attribution.
		if spouseKeycloakID != "" && sums.totalNIS > 12*basePriceNIS {
			log.Warn("EvaluateV2Price: BETTER_OF_ALONE",
				slog.String("keycloak_id", primaryKeycloakID),
				slog.String("spouse_keycloak_id", spouseKeycloakID),
			)
		}
	}

	inputs.Explain = buildExplain(inputs)
	return inputs, nil
}

// buildDonationsDiscount returns the Discount record for the donations discount type
// and a bool indicating whether the primary account receives the discounted price.
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
) (Discount, bool) {
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

	return Discount{
		Type:       DiscountTypeDonations,
		AmountPct:  DonationsDiscountAmtPct,
		Eligible:   eligible,
		Properties: propsJSON,
	}, primaryGetsDiscount
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

// toNIS converts an amount in the given currency to NIS using the provided rates.
// Unknown currencies are treated as USD (conservative fallback).
func toNIS(amount float64, currency string, usdRate, eurRate float64) float64 {
	switch strings.ToUpper(currency) {
	case common.CurrencyNIS:
		return amount
	case common.CurrencyUSD:
		return amount * usdRate
	case common.CurrencyEUR:
		return amount * eurRate
	default:
		return amount * usdRate
	}
}

// buildExplain produces a human-readable pseudo-code description of the v2 pricing logic
// as it was applied for this evaluation. Donation amounts and NIS totals are never included.
// All prices are shown in the account's base currency; NIS conversion is acknowledged but
// rates and converted amounts are never revealed.
func buildExplain(inputs *V2PricingEvaluation) []string {
	base := inputs.CountryBase
	annualBase := base.Amount * 12
	cur := base.Currency

	var props DonationsDiscountProperties
	for _, d := range inputs.Discounts {
		if d.Type == DiscountTypeDonations {
			_ = json.Unmarshal(d.Properties, &props)
			break
		}
	}

	var lines [5]string

	// Step 1: country tier — original currency only, no NIS conversion shown
	lines[0] = fmt.Sprintf("1. country[%s %q] → tier lookup → %s → base: %.2f %s/mo × 12 = %.2f %s/yr",
		inputs.CountryCode, GetCountryName(inputs.CountryCode), base.Group,
		base.Amount, cur, annualBase, cur)

	// Step 2: email collection and Priority fetch
	fetchStatus := "ok"
	if props.DonationsFetchNote != "" {
		fetchStatus += ": " + props.DonationsFetchNote
	}
	hasSpouse := props.SpouseKeycloakID != ""
	if hasSpouse {
		total := props.PrimaryEmailCount + props.SpouseEmailCount
		lines[1] = fmt.Sprintf("2. collect emails: primary(%d) + spouse(%d) = %d unique → fetch from Priority ERP (SKU=40001, last 12mo) → %s",
			props.PrimaryEmailCount, props.SpouseEmailCount, total, fetchStatus)
	} else {
		lines[1] = fmt.Sprintf("2. collect emails: primary(%d) → fetch from Priority ERP (SKU=40001, last 12mo) → %s",
			props.PrimaryEmailCount, fetchStatus)
	}

	// Step 3: aggregation — acknowledge NIS conversion but show no amounts
	lines[2] = "3. sum all donations per currency → convert each to NIS"

	// Step 4: threshold logic — thresholds in original currency; append (→ NIS) only when not already NIS
	nisMarker := ""
	if cur != common.CurrencyNIS {
		nisMarker = " (→ NIS)"
	}
	primaryGetsDiscount := inputs.FinalPrice.Amount < base.Amount
	discountPct := int(DonationsDiscountAmtPct)
	switch {
	case hasSpouse && primaryGetsDiscount:
		lines[3] = fmt.Sprintf("4. combined >= %.2f %s/yr%s (2× annual) → both members get %d%% off",
			annualBase*2, cur, nisMarker, discountPct)
	case hasSpouse:
		lines[3] = fmt.Sprintf("4. combined < %.2f %s/yr%s (2× annual) → no discount",
			annualBase*2, cur, nisMarker)
	case primaryGetsDiscount:
		lines[3] = fmt.Sprintf("4. combined >= %.2f %s/yr%s → primary gets %d%% off",
			annualBase, cur, nisMarker, discountPct)
	default:
		lines[3] = fmt.Sprintf("4. combined < %.2f %s/yr%s → no discount",
			annualBase, cur, nisMarker)
	}

	// Step 5: final prices in original currency
	if hasSpouse {
		spousePrice := base.Amount
		if props.SpouseGetsDiscount {
			spousePrice = base.Amount * (1 - DonationsDiscountAmtPct/100)
		}
		lines[4] = fmt.Sprintf("5. primary[#%d]: %.2f %s | spouse: %.2f %s",
			inputs.AccountID, inputs.FinalPrice.Amount, inputs.FinalPrice.Currency,
			spousePrice, cur)
	} else {
		lines[4] = fmt.Sprintf("5. primary[#%d]: %.2f %s",
			inputs.AccountID, inputs.FinalPrice.Amount, inputs.FinalPrice.Currency)
	}

	return lines[:]
}
