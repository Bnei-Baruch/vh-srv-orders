package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// DiscountType identifies a discount by name.
type DiscountType string

const DiscountTypeDonations DiscountType = "donations"

// Discount is a generic discount record. Properties holds type-specific data as JSON.
type Discount struct {
	Type       DiscountType    `json:"type"`
	AmountPct  float64         `json:"amount_pct"` // e.g. 50.0 for 50% off
	Eligible   bool            `json:"eligible"`
	Error      bool            `json:"error,omitempty"`
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
	SpouseAccountID    int    `json:"spouse_account_id,omitempty"`
	SpouseKeycloakID   string `json:"spouse_keycloak_id,omitempty"`
	SpouseGetsDiscount bool   `json:"spouse_gets_discount,omitempty"`

	// Priority fetch metadata
	DonationsFetched       bool     `json:"donations_fetched"`
	DonationsFetchedEmails []string `json:"donations_fetched_emails,omitempty"`
	DonationsFetchNote     string   `json:"donations_fetch_note,omitempty"`
	DonationsFetchError    string   `json:"donations_fetch_error,omitempty"`

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
			Error:     d.Error,
		}
	}
	return &pub
}

// AccountRepository is the minimal interface EvaluateV2Price needs from the repo layer.
type AccountRepository interface {
	GetAccountIDByKeycloakID(ctx context.Context, keycloakID string) (int, error)
	GetEmailByKeycloakID(ctx context.Context, keycloakID string) (string, error)
}

// donationSums holds aggregated Priority ERP donation data.
// Used only during v2 price calculation — never stored, logged, or returned.
type donationSums struct {
	perCurrency   map[string]float64
	totalNIS      float64
	fetched       bool
	successEmails []string // emails that returned data from Priority without error
	fetchNote     string   // informational (e.g. "customer not found" for some emails)
	fetchError    string   // real API errors; non-empty means price must not be trusted
}

// EvaluateV2Price computes the full v2 pricing evaluation for a member account.
//
// Donation amounts fetched from Priority ERP exist only in local variables
// within this function. They must never be stored, logged, or returned.
func EvaluateV2Price(
	ctx context.Context,
	accounts AccountRepository,
	profileService profiles.ProfileService,
	priorityClient *priority.Client,
	primaryAccountID int,
	primaryKeycloakID string,
	primaryEmail string,
	country string,
) (*V2PricingEvaluation, error) {
	log := utils.LogFor(ctx)
	log.Info("EvaluateV2Price: start", slog.Int("account_id", primaryAccountID), slog.String("country", country))

	base := GetCountryBasePrice(country)
	inputs := &V2PricingEvaluation{
		EvaluatedAt: time.Now().UTC(),
		AccountID:   primaryAccountID,
		CountryCode: country,
		CountryBase: base,
	}

	// Fetch primary profile to get all emails and spouse info.
	// Soft-fail: if profile service is unavailable, fall back to account email.
	log.Info("EvaluateV2Price: fetching primary profile")
	profile, err := profileService.GetProfileByKeycloakID(ctx, primaryKeycloakID)
	if err != nil && !errors.Is(err, profiles.ErrNotFound) {
		return nil, fmt.Errorf("profileService.GetProfileByKeycloakID: %w", err)
	}
	primaryEmails := collectProfileEmails(profile, primaryEmail)
	primaryEmailCount := len(primaryEmails)
	log.Info("EvaluateV2Price: primary profile done", slog.Int("email_count", primaryEmailCount))

	// Collect spouse emails if a spouse is linked.
	var (
		spouseEmails     []string
		spouseAccountID  int
		spouseKeycloakID string
		spouseEmailCount int
	)
	if profile != nil && profile.SpouseKeycloakID != nil && *profile.SpouseKeycloakID != "" {
		spouseKeycloakID = *profile.SpouseKeycloakID

		log.Info("EvaluateV2Price: fetching spouse account")
		spouseAccountID, err = accounts.GetAccountIDByKeycloakID(ctx, spouseKeycloakID)
		if err == nil {
			spouseEmail, _ := accounts.GetEmailByKeycloakID(ctx, spouseKeycloakID)
			log.Info("EvaluateV2Price: fetching spouse profile", slog.Int("spouse_account_id", spouseAccountID))
			spouseProfile, err := profileService.GetProfileByKeycloakID(ctx, spouseKeycloakID)
			if err != nil && !errors.Is(err, profiles.ErrNotFound) {
				return nil, fmt.Errorf("profileService.GetProfileByKeycloakID (spouse): %w", err)
			}
			spouseEmails = collectProfileEmails(spouseProfile, spouseEmail)
			spouseEmailCount = len(spouseEmails)
			log.Info("EvaluateV2Price: spouse profile done", slog.Int("email_count", spouseEmailCount))
		} else {
			log.Info("EvaluateV2Price: spouse account not found", slog.Any("err", err))
		}
	}

	// Fetch donations once with all emails deduplicated across primary and spouse.
	// Amounts stay in local variable only — never persisted or returned.
	emails := deduplicateEmails(primaryEmails, spouseEmails)
	log.Info("EvaluateV2Price: fetching donations", slog.Int("email_count", len(emails)))
	sums := fetchDonationSums(ctx, priorityClient, emails, USDToNIS, EURToNIS)
	log.Info("EvaluateV2Price: donations done",
		slog.Bool("fetched", sums.fetched),
		slog.String("note", sums.fetchNote),
		slog.String("error", sums.fetchError),
	)

	basePriceNIS := toNIS(base.Amount, base.Currency, USDToNIS, EURToNIS)

	discount, primaryGetsDiscount := buildDonationsDiscount(
		sums, base, basePriceNIS,
		primaryAccountID, spouseAccountID,
		primaryEmailCount, spouseEmailCount,
		spouseKeycloakID,
	)
	inputs.Discounts = []Discount{discount}

	if primaryGetsDiscount {
		inputs.FinalPrice = Price{Amount: base.Amount / 2, Currency: base.Currency}
	} else {
		inputs.FinalPrice = Price{Amount: base.Amount, Currency: base.Currency}
	}

	inputs.Explain = buildExplain(inputs)
	return inputs, nil
}

// buildDonationsDiscount returns the Discount record for the donations discount type
// and a bool indicating whether the primary account receives the discounted price.
func buildDonationsDiscount(
	sums donationSums,
	base CountryBasePrice,
	basePriceNIS float64,
	primaryAccountID, spouseAccountID int,
	primaryEmailCount, spouseEmailCount int,
	spouseKeycloakID string,
) (Discount, bool) {
	annualNIS := basePriceNIS * 12
	half := annualNIS / 2
	hasSpouse := spouseAccountID > 0

	eligible := sums.totalNIS >= half

	var primaryGetsDiscount, spouseGetsDiscount bool
	switch {
	case !eligible:
		// no discount
	case sums.totalNIS >= annualNIS:
		primaryGetsDiscount = true
		spouseGetsDiscount = hasSpouse
	default: // half <= totalNIS < annualNIS — one person gets it
		if hasSpouse && spouseAccountID < primaryAccountID {
			spouseGetsDiscount = true
		} else {
			primaryGetsDiscount = true
		}
	}

	props := DonationsDiscountProperties{
		PrimaryEmailCount:      primaryEmailCount,
		SpouseEmailCount:       spouseEmailCount,
		SpouseAccountID:        spouseAccountID,
		SpouseKeycloakID:       spouseKeycloakID,
		SpouseGetsDiscount:     spouseGetsDiscount,
		DonationsFetched:       sums.fetched,
		DonationsFetchedEmails: sums.successEmails,
		DonationsFetchNote:     sums.fetchNote,
		DonationsFetchError:    sums.fetchError,
		AnnualBase:             Price{Amount: base.Amount * 12, Currency: base.Currency},
	}
	propsJSON, _ := json.Marshal(props)

	return Discount{
		Type:       DiscountTypeDonations,
		AmountPct:  50.0,
		Eligible:   eligible,
		Error:      sums.fetchError != "",
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
// Donation amounts in the returned struct must never be stored, logged, or returned.
func fetchDonationSums(ctx context.Context, client *priority.Client, emails []string, usdRate, eurRate float64) donationSums {
	result := donationSums{
		perCurrency: make(map[string]float64),
		fetched:     true,
	}

	var notFoundNotes, apiErrors []string
	for _, email := range emails {
		contributions, err := client.GetLastContributions(ctx, email)
		if err != nil {
			if strings.Contains(err.Error(), "customer not found") || strings.Contains(err.Error(), "no active customers found") {
				notFoundNotes = append(notFoundNotes, fmt.Sprintf("%s: %v", email, err))
				continue // no Priority account for this email — treat as zero donations
			}
			apiErrors = append(apiErrors, fmt.Sprintf("%s: %v", email, err))
			continue
		}
		result.successEmails = append(result.successEmails, email)
		for currency, amount := range contributions {
			result.perCurrency[currency] += amount
		}
	}

	if len(notFoundNotes) > 0 {
		result.fetchNote = fmt.Sprintf("no Priority account for: %s", strings.Join(notFoundNotes, ", "))
	}
	if len(apiErrors) > 0 {
		result.fetchError = strings.Join(apiErrors, "; ")
		if len(apiErrors) == len(emails) {
			result.fetched = false
		}
	}

	for currency, amount := range result.perCurrency {
		result.totalNIS += toNIS(amount, currency, usdRate, eurRate)
	}

	return result
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
	halfBase := annualBase / 2
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
	if !props.DonationsFetched {
		fetchStatus = "failed"
	}
	if props.DonationsFetchError != "" {
		fetchStatus += " ERROR: " + props.DonationsFetchError
	} else if props.DonationsFetchNote != "" {
		fetchStatus += ": " + props.DonationsFetchNote
	}
	if props.SpouseAccountID > 0 {
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
	switch {
	case props.SpouseAccountID > 0 && primaryGetsDiscount && props.SpouseGetsDiscount:
		lines[3] = fmt.Sprintf("4. combined >= %.2f %s/yr%s → both members get 50%% off",
			annualBase, cur, nisMarker)
	case props.SpouseAccountID > 0 && props.SpouseGetsDiscount:
		lines[3] = fmt.Sprintf("4. %.2f %s/yr%s <= combined < %.2f %s/yr%s, spouse_id[%d] < primary_id[%d] → spouse gets 50%% off",
			halfBase, cur, nisMarker, annualBase, cur, nisMarker, props.SpouseAccountID, inputs.AccountID)
	case primaryGetsDiscount && props.SpouseAccountID > 0:
		lines[3] = fmt.Sprintf("4. %.2f %s/yr%s <= combined < %.2f %s/yr%s, primary_id[%d] <= spouse_id[%d] → primary gets 50%% off",
			halfBase, cur, nisMarker, annualBase, cur, nisMarker, inputs.AccountID, props.SpouseAccountID)
	case primaryGetsDiscount:
		lines[3] = fmt.Sprintf("4. combined >= %.2f %s/yr%s → primary gets 50%% off",
			halfBase, cur, nisMarker)
	default:
		lines[3] = fmt.Sprintf("4. combined < %.2f %s/yr%s → no discount",
			halfBase, cur, nisMarker)
	}

	// Step 5: final prices in original currency
	spousePrice := base.Amount
	if props.SpouseGetsDiscount {
		spousePrice = base.Amount / 2
	}
	if props.SpouseAccountID > 0 {
		lines[4] = fmt.Sprintf("5. primary[#%d]: %.2f %s | spouse[#%d]: %.2f %s",
			inputs.AccountID, inputs.FinalPrice.Amount, inputs.FinalPrice.Currency,
			props.SpouseAccountID, spousePrice, cur)
	} else {
		lines[4] = fmt.Sprintf("5. primary[#%d]: %.2f %s",
			inputs.AccountID, inputs.FinalPrice.Amount, inputs.FinalPrice.Currency)
	}

	return lines[:]
}
