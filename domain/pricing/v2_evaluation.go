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
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// Currency conversion ratios to NIS (hardcoded — update when rates change significantly).
const (
	USDToNIS = 3.1 // 1 USD = 3.1 NIS
	EURToNIS = 3.6 // 1 EUR = 3.6 NIS
)

// ErrDonationFetch is returned when a real API error (not "user not found") occurs
// while fetching donation data from any source. The price must not be used when this occurs.
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
	Error      bool            `json:"error,omitempty"` // true when the discount's data source failed
	Properties json.RawMessage `json:"properties,omitempty"`
}

// DonationsDiscountProperties holds all data specific to the donations discount type.
//
// Donation amounts fetched from any source are deliberately excluded — they are
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

// HasDiscountErrors returns true if any discount's data source failed during evaluation.
// When true, the FinalPrice cannot be trusted for billing or display.
func (e *V2PricingEvaluation) HasDiscountErrors() bool {
	for _, d := range e.Discounts {
		if d.Error {
			return true
		}
	}
	return false
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
			Error:     d.Error,
		}
	}
	return &pub
}

// donationSums holds aggregated donation data across all sources (Priority,
// QuickBooks, and European donations via vh-srv-accounting).
// Used only during v2 price calculation — never stored, logged, or returned.
type donationSums struct {
	perCurrency   map[string]float64
	totalNIS      float64
	successEmails []string // emails that returned data from at least one source without error
	fetchNote     string   // informational (per-source "no record for: ..." markers)
}

// EvaluateV2Price computes the full v2 pricing evaluation for a member account.
//
// Donation amounts fetched from any source exist only in local variables
// within this function. They must never be stored, logged, or returned.
func EvaluateV2Price(
	ctx context.Context,
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	accountingService accounting.AccountingService,
	quickbooksCompanyID string,
	primaryAccountID int,
	primaryKeycloakID string,
	primaryEmail string,
	country string,
	discountProvider repo.ManualDiscountProvider,
) (*V2PricingEvaluation, error) {
	ctx = context.WithValue(ctx, common.CtxLogger, utils.LogFor(ctx).With(
		slog.Int("account_id", primaryAccountID),
		slog.String("keycloak_id", primaryKeycloakID),
	))
	log := utils.LogFor(ctx)
	log.Info("EvaluateV2Price: start", slog.String("country", country))

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
	sums, fetchErr := fetchDonationSums(ctx, priorityClient, accountingService, quickbooksCompanyID, emails, USDToNIS, EURToNIS)

	var donationsDiscount Discount
	var primaryGetsDiscount bool

	if fetchErr != nil {
		log.Warn("EvaluateV2Price: donation fetch failed, recording error on discount",
			slog.Any("err", fetchErr),
		)
		donationsDiscount = Discount{
			Type:      DiscountTypeDonations,
			AmountPct: DonationsDiscountAmtPct,
			Eligible:  false,
			Error:     true,
		}
	} else {
		log.Info("EvaluateV2Price: fetched donations",
			slog.Int("success_email_count", len(sums.successEmails)),
			slog.String("note", sums.fetchNote),
		)
		basePriceNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS)
		donationsDiscount, primaryGetsDiscount = buildDonationsDiscount(
			sums, base, basePriceNIS,
			primaryEmailCount, spouseEmailCount,
			spouseKeycloakID,
		)
		// A log marker for edge case where one is better off alone than with a spouse.
		if spouseKeycloakID != "" && !primaryGetsDiscount && sums.totalNIS > 12*basePriceNIS {
			log.Warn("EvaluateV2Price: BETTER_OF_ALONE",
				slog.String("keycloak_id", primaryKeycloakID),
				slog.String("spouse_keycloak_id", spouseKeycloakID),
			)
		}
	}

	inputs.Discounts = []Discount{donationsDiscount}

	if primaryGetsDiscount {
		inputs.FinalPrice = Price{Amount: math.Round(base.Amount*(1-DonationsDiscountAmtPct/100)*100) / 100, Currency: base.Currency}
	} else {
		inputs.FinalPrice = Price{Amount: base.Amount, Currency: base.Currency}
	}

	inputs.Explain = buildExplain(inputs)

	if discountProvider != nil && primaryKeycloakID != "" {
		md, mdErr := discountProvider.GetActiveManualDiscount(ctx, primaryKeycloakID)
		if mdErr != nil {
			log.Warn("EvaluateV2Price: manual discount fetch failed, recording error on discount",
				slog.Any("err", mdErr),
			)
			inputs.Discounts = append(inputs.Discounts, Discount{
				Type:  DiscountTypeManual,
				Error: true,
			})
			inputs.Explain = append(inputs.Explain, "manual_discount: fetch error — not applied")
		} else {
			applyManualDiscount(ctx, inputs, md)
		}
	}

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

// fetchDonationSums aggregates donations across all configured sources (Priority ERP
// and vh-srv-accounting: QuickBooks and European donations) for the given emails.
// "User not found" responses from any source are treated as zero donations. Any real API error from any source is
// returned immediately — the price must not be used. Donation amounts in the returned
// struct must never be stored, logged, or returned.
//
// Sources are queried in sequence so each block can be removed cleanly when its source
// is decommissioned (Priority will eventually migrate behind vh-srv-accounting).
func fetchDonationSums(
	ctx context.Context,
	priorityClient *priority.Client,
	accountingService accounting.AccountingService,
	quickbooksCompanyID string,
	emails []string,
	usdRate, eurRate float64,
) (donationSums, error) {
	result := donationSums{perCurrency: make(map[string]float64)}
	successSet := make(map[string]struct{})
	var notes []string

	// Source: Priority ERP. (TODO: remove when Priority migrates into vh-srv-accounting.)
	priorityNotFound, err := addPriorityContributions(ctx, priorityClient, emails, result.perCurrency, successSet)
	if err != nil {
		return donationSums{}, err
	}
	if len(priorityNotFound) > 0 {
		notes = append(notes, fmt.Sprintf("no Priority record for: %s", strings.Join(priorityNotFound, ", ")))
	}

	// Source: vh-srv-accounting (QuickBooks).
	accountingNotFound, err := addAccountingContributions(ctx, accountingService, quickbooksCompanyID, emails, result.perCurrency, successSet)
	if err != nil {
		return donationSums{}, err
	}
	if len(accountingNotFound) > 0 {
		notes = append(notes, fmt.Sprintf("no QuickBooks record for: %s", strings.Join(accountingNotFound, ", ")))
	}

	// Source: vh-srv-accounting (European donations, batch).
	europeNotFound, err := addEuropeContributions(ctx, accountingService, emails, result.perCurrency, successSet)
	if err != nil {
		return donationSums{}, err
	}
	if len(europeNotFound) > 0 {
		notes = append(notes, fmt.Sprintf("no Europe record for: %s", strings.Join(europeNotFound, ", ")))
	}

	// Preserve input order in successEmails.
	for _, email := range emails {
		if _, ok := successSet[email]; ok {
			result.successEmails = append(result.successEmails, email)
		}
	}
	result.fetchNote = strings.Join(notes, "; ")

	for currency, amount := range result.perCurrency {
		result.totalNIS += toNIS(amount, currency, usdRate, eurRate)
	}

	return result, nil
}

// addPriorityContributions queries Priority for each email and accumulates currency sums.
// Returns the list of emails with no Priority record (ErrNoActiveCustomers).
func addPriorityContributions(
	ctx context.Context,
	client *priority.Client,
	emails []string,
	perCurrency map[string]float64,
	successSet map[string]struct{},
) ([]string, error) {
	var notFound []string
	for _, email := range emails {
		contributions, err := client.GetLastContributions(ctx, email)
		if err != nil {
			if errors.Is(err, priority.ErrNoActiveCustomers) {
				notFound = append(notFound, email)
				continue
			}
			return nil, fmt.Errorf("priorityClient.GetLastContributions %w: %s: %v", ErrDonationFetch, email, err)
		}
		successSet[email] = struct{}{}
		for currency, amount := range contributions {
			perCurrency[currency] += amount
		}
	}
	return notFound, nil
}

// addAccountingContributions queries vh-srv-accounting for each email and accumulates
// currency sums. Returns the list of emails not found in the configured QB company.
func addAccountingContributions(
	ctx context.Context,
	client accounting.AccountingService,
	companyID string,
	emails []string,
	perCurrency map[string]float64,
	successSet map[string]struct{},
) ([]string, error) {
	var companyIDPtr *string
	if companyID != "" {
		companyIDPtr = &companyID
	}

	var notFound []string
	for _, email := range emails {
		res, err := client.GetLastContributions(ctx, email, companyIDPtr)
		if err != nil {
			return nil, fmt.Errorf("accountingService.GetLastContributions %w: %s: %v", ErrDonationFetch, email, err)
		}
		if !res.Found {
			notFound = append(notFound, email)
			continue
		}
		successSet[email] = struct{}{}
		for currency, amount := range res.Total {
			perCurrency[currency] += amount
		}
	}
	return notFound, nil
}

// addEuropeContributions queries the European donations system with a single
// batch call and accumulates currency sums. Returns the emails with no Europe
// record. Amounts may be negative (refunds subtracted upstream).
//
// NOTE: a donor present in more than one source under the same email is summed
// across sources — same known limitation as shared emails between users.
func addEuropeContributions(
	ctx context.Context,
	client accounting.AccountingService,
	emails []string,
	perCurrency map[string]float64,
	successSet map[string]struct{},
) ([]string, error) {
	if len(emails) == 0 {
		return nil, nil
	}

	res, err := client.GetEuropeContributions(ctx, emails)
	if err != nil {
		return nil, fmt.Errorf("accountingService.GetEuropeContributions %w: %v", ErrDonationFetch, err)
	}

	entryByEmail := make(map[string]accounting.EuropeContributionEntry, len(res.Results))
	for _, entry := range res.Results {
		entryByEmail[strings.ToLower(entry.Identifier)] = entry
	}

	// Match entries back to the request emails case-insensitively so successSet
	// stays keyed on the original-cased input, like the other sources. A missing
	// entry is treated as not found rather than an error.
	var notFound []string
	for _, email := range emails {
		entry, ok := entryByEmail[strings.ToLower(email)]
		if !ok || !entry.Found {
			notFound = append(notFound, email)
			continue
		}
		successSet[email] = struct{}{}
		for currency, amount := range entry.Contributions {
			perCurrency[currency] += amount
		}
	}
	return notFound, nil
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

	var donationsDiscount Discount
	var props DonationsDiscountProperties
	for _, d := range inputs.Discounts {
		if d.Type == DiscountTypeDonations {
			donationsDiscount = d
			_ = json.Unmarshal(d.Properties, &props)
			break
		}
	}

	if donationsDiscount.Error {
		return []string{
			fmt.Sprintf("1. country[%s %q] → tier lookup → %s → base: %.2f %s/mo × 12 = %.2f %s/yr",
				inputs.CountryCode, GetCountryName(inputs.CountryCode), base.Group,
				base.Amount, cur, annualBase, cur),
			"2. donation data unavailable — price not discounted",
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
		lines[1] = fmt.Sprintf("2. collect emails: primary(%d) + spouse(%d) = %d unique → fetch donations from all sources (Priority, QuickBooks, Europe; last 12mo) → %s",
			props.PrimaryEmailCount, props.SpouseEmailCount, total, fetchStatus)
	} else {
		lines[1] = fmt.Sprintf("2. collect emails: primary(%d) → fetch donations from all sources (Priority, QuickBooks, Europe; last 12mo) → %s",
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
