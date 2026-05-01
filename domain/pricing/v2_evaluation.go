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

// DiscountType identifies a discount by name.
type DiscountType string

const (
	DiscountTypeDonations   DiscountType = "donations"
	DiscountTypeHH          DiscountType = "Help Haver"
	DonationsDiscountAmtPct              = 55.0 // percent off: final price = base * (1 - DonationsDiscountAmtPct/100)
)

// Discount is a generic discount record. Properties holds type-specific data as JSON.
// Applied is true when this discount determined the final price (relevant when multiple discounts compete).
type Discount struct {
	Type       DiscountType    `json:"type"`
	AmountPct  float64         `json:"amount_pct,omitempty"` // percent off (e.g. 55.0 means pay 45%)
	Eligible   bool            `json:"eligible"`
	Applied    bool            `json:"applied"`
	Properties json.RawMessage `json:"properties,omitempty"`
}

// HHDiscountProperties holds grant-specific data for the "Help Haver" discount.
type HHDiscountProperties struct {
	ExpiresAt time.Time `json:"expires_at"` // grant expiry: grant.created_at + months
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
			Applied:   d.Applied,
		}
	}
	return &pub
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

	discount, primaryGetsDiscount, donationsLines := buildDonationsDiscount(
		sums, base, basePriceNIS,
		primaryEmailCount, spouseEmailCount,
		spouseKeycloakID,
	)
	inputs.Discounts = []Discount{discount}

	countryLine := fmt.Sprintf("country[%s] → %s → %.2f %s/mo",
		country, base.Group, base.Amount, base.Currency)

	// Log edge case: couple where individual threshold is met but not the couple threshold.
	// TODO: revisit this logic when we have a better way to distinguish attribution.
	if spouseKeycloakID != "" && !primaryGetsDiscount && sums.totalNIS > 12*basePriceNIS {
		log.Warn("EvaluateV2Price: BETTER_OF_ALONE",
			slog.String("keycloak_id", primaryKeycloakID),
			slog.String("spouse_keycloak_id", spouseKeycloakID),
		)
	}

	// Add HH grant as an independent discount if available. Applied is not set here;
	// resolveFinalPrice selects the winner by NIS comparison and sets Applied on it.
	hhGrant, err := profileService.GetActiveHHGrant(ctx, primaryKeycloakID)
	if err != nil {
		log.Error("EvaluateV2Price: GetActiveHHGrant failed — skipping HH discount", slog.Any("err", err))
	} else if hhGrant != nil && !hhGrant.IsExpired() {
		inputs.Discounts = append(inputs.Discounts, buildHHDiscount(hhGrant))
	}

	// Pick the best eligible discount: converts each to NIS, sets Applied on the winner.
	inputs.FinalPrice = resolveFinalPrice(inputs.Discounts, base.Price, USDToNIS, EURToNIS)

	// Build explain: country line, then per-discount evaluation lines, then final price line.
	// The final loop runs after resolveFinalPrice so each discount's Applied flag is set.
	explain := make([]string, 0, 3+len(donationsLines))
	explain = append(explain, countryLine)
	explain = append(explain, donationsLines...)
	for _, d := range inputs.Discounts {
		if d.Type == DiscountTypeHH {
			explain = append(explain, hhExplainLine(d))
		}
	}
	if spouseKeycloakID != "" {
		spousePrice := base.Amount
		if discount.Eligible {
			spousePrice = math.Round(base.Amount*(1-DonationsDiscountAmtPct/100)*100) / 100
		}
		explain = append(explain, fmt.Sprintf("final: primary[#%d] %.2f %s | spouse %.2f %s",
			primaryAccountID, inputs.FinalPrice.Amount, inputs.FinalPrice.Currency,
			spousePrice, base.Currency))
	} else {
		explain = append(explain, fmt.Sprintf("final: primary[#%d] %.2f %s",
			primaryAccountID, inputs.FinalPrice.Amount, inputs.FinalPrice.Currency))
	}
	inputs.Explain = explain
	return inputs, nil
}

// resolveFinalPrice picks the eligible discount with the lowest NIS-equivalent price,
// marks it Applied, and returns the resulting price. Each discount is compared against base —
// the one that gives the biggest absolute saving wins. If no eligible discount beats base,
// base is returned unchanged.
func resolveFinalPrice(discounts []Discount, base Price, usdToNIS, eurToNIS float64) Price {
	bestNIS := toNIS(base.Amount, base.Currency, usdToNIS, eurToNIS)
	bestPrice := base
	bestIdx := -1

	for i := range discounts {
		if !discounts[i].Eligible {
			continue
		}
		effectiveNIS := toNIS(base.Amount*(1-discounts[i].AmountPct/100), base.Currency, usdToNIS, eurToNIS)
		if effectiveNIS < bestNIS {
			bestNIS = effectiveNIS
			bestPrice = Price{
				Amount:   math.Round(base.Amount*(100-discounts[i].AmountPct)) / 100,
				Currency: base.Currency,
			}
			bestIdx = i
		}
	}

	if bestIdx >= 0 {
		discounts[bestIdx].Applied = true
	}
	return bestPrice
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
