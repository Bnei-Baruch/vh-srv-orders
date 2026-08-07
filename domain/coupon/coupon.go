// Package coupon holds the coupon business rules that do not touch the database:
// request validation, code generation, and benefit-window computation. It is
// deliberately free of repo and pricing imports so the repo layer can depend on
// it without an import cycle (repo ← coupon; pricing ← coupon audit is elsewhere).
package coupon

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	TypePercent    = "percent"
	TypeFixedPrice = "fixed_price"

	// codeAlphabet excludes visually ambiguous characters (0/O, 1/I/L, B/8, S/5).
	codeAlphabet      = "ACDEFGHJKLMNPQRTUVWXYZ234679"
	codeSuffixLen     = 5
	DefaultRedeemDays = 30

	// JerusalemTZ is the billing timezone for calendar-month coloring.
	JerusalemTZ = "Asia/Jerusalem"
)

// Fields is the plain-value view of a coupon that Validate checks. Callers map
// their request/row types onto it so this package never imports repo.
type Fields struct {
	Type           string
	DiscountPct    *float64
	FixedPrice     *float64
	FixedCurrency  *string
	RedeemFrom     time.Time
	RedeemUntil    time.Time
	BenefitStart   *time.Time
	BenefitEnd     *time.Time
	BenefitMonths  *int
	Countries      []string
	MaxRedemptions int
}

// Validate enforces the locked business rules (design §1). Countries is an
// optional redemption scope for both types; fixed_price coupons are NOT tied to
// countries — the pricing evaluation converts the coupon currency to the
// member's currency, so any country/currency combination is valid.
func Validate(f Fields) error {
	switch f.Type {
	case TypePercent:
		if f.DiscountPct == nil || *f.DiscountPct < 1 || *f.DiscountPct > 99 {
			return fmt.Errorf("percent coupon requires discount_pct in 1..99")
		}
	case TypeFixedPrice:
		if f.FixedPrice == nil || *f.FixedPrice <= 0 {
			return fmt.Errorf("fixed_price coupon requires fixed_price > 0")
		}
		if f.FixedCurrency == nil || *f.FixedCurrency == "" {
			return fmt.Errorf("fixed_price coupon requires currency")
		}
	default:
		return fmt.Errorf("type must be %q or %q", TypePercent, TypeFixedPrice)
	}

	if f.MaxRedemptions < 1 {
		return fmt.Errorf("max_redemptions must be at least 1")
	}
	if !f.RedeemFrom.IsZero() && !f.RedeemFrom.Before(f.RedeemUntil) {
		return fmt.Errorf("redeem_from must be before redeem_until")
	}

	offset := f.BenefitMonths != nil && *f.BenefitMonths > 0
	window := f.BenefitStart != nil && f.BenefitEnd != nil && f.BenefitStart.Before(*f.BenefitEnd)
	if offset == window {
		return fmt.Errorf("exactly one benefit mode required: benefit_months > 0, or benefit_start < benefit_end")
	}
	if window && f.RedeemUntil.After(*f.BenefitEnd) {
		return fmt.Errorf("redeem_until must not be after benefit_end")
	}
	return nil
}

// GenerateCode returns PREFIX-XXXXX with a cryptographically-random suffix drawn
// from the unambiguous alphabet. Prefix is uppercased and trimmed.
func GenerateCode(prefix string) (string, error) {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if prefix == "" {
		return "", fmt.Errorf("prefix is required")
	}
	for i, r := range prefix {
		isLetter := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isLetter {
			return "", fmt.Errorf("prefix must start with an English letter")
		}
		if !isLetter && !isDigit && r != '-' {
			return "", fmt.Errorf("prefix may contain only English letters, digits and hyphens")
		}
	}
	alphabetLen := big.NewInt(int64(len(codeAlphabet)))
	suffix := make([]byte, codeSuffixLen)
	for i := range suffix {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("rand.Int: %w", err)
		}
		suffix[i] = codeAlphabet[n.Int64()]
	}
	return prefix + "-" + string(suffix), nil
}

// BenefitWindow computes the denormalized [start, end) window at redemption.
// Mode A copies the fixed dates; mode B colors whole calendar months in the
// billing timezone: [first day of now's month, first day + months).
func BenefitWindow(months *int, start, end *time.Time, now time.Time) (time.Time, time.Time, error) {
	if start != nil && end != nil {
		return *start, *end, nil
	}
	if months == nil || *months <= 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("no benefit mode set")
	}
	loc, err := time.LoadLocation(JerusalemTZ)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("time.LoadLocation: %w", err)
	}
	n := now.In(loc)
	monthStart := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	return monthStart, monthStart.AddDate(0, *months, 0), nil
}
