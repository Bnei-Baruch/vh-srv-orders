package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/coupon"
)

// CouponProvider fetches a member's active coupon redemptions for pricing.
// Implemented by *OrdersDB; injected into the pricing evaluation.
type CouponProvider interface {
	GetActiveCouponRedemptions(ctx context.Context, keycloakID string) ([]ActiveCouponRedemption, error)
}

const couponColumns = `id, code, description, type, properties, enabled, redeem_from, redeem_until,
	benefit_start, benefit_end, benefit_months, countries, max_redemptions, created_at, updated_at`

const couponColumnsC = `c.id, c.code, c.description, c.type, c.properties, c.enabled, c.redeem_from,
	c.redeem_until, c.benefit_start, c.benefit_end, c.benefit_months, c.countries, c.max_redemptions,
	c.created_at, c.updated_at`

func scanCoupon(row pgx.Row, c *Coupon) error {
	return row.Scan(&c.ID, &c.Code, &c.Description, &c.Type, &c.Properties, &c.Enabled,
		&c.RedeemFrom, &c.RedeemUntil, &c.BenefitStart, &c.BenefitEnd, &c.BenefitMonths,
		&c.Countries, &c.MaxRedemptions, &c.CreatedAt, &c.UpdatedAt)
}

func (o *OrdersDB) CreateCoupon(ctx context.Context, c Coupon) (*Coupon, error) {
	err := o.QueryRow(ctx,
		`INSERT INTO coupons (code, description, type, properties, enabled, redeem_from, redeem_until,
			benefit_start, benefit_end, benefit_months, countries, max_redemptions)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING `+couponColumns,
		c.Code, c.Description, c.Type, c.Properties, c.Enabled, c.RedeemFrom, c.RedeemUntil,
		c.BenefitStart, c.BenefitEnd, c.BenefitMonths, c.Countries, c.MaxRedemptions,
	).Scan(&c.ID, &c.Code, &c.Description, &c.Type, &c.Properties, &c.Enabled,
		&c.RedeemFrom, &c.RedeemUntil, &c.BenefitStart, &c.BenefitEnd, &c.BenefitMonths,
		&c.Countries, &c.MaxRedemptions, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, common.ErrCouponCodeConflict
		}
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}
	return &c, nil
}

func (o *OrdersDB) GetCouponByID(ctx context.Context, id int) (*Coupon, error) {
	var c Coupon
	if err := scanCoupon(o.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons WHERE id = $1`, id), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, common.ErrNoRowsAffected
		}
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}
	return &c, nil
}

// ListCoupons returns every coupon with its redemption count (all rows, revoked
// included) and benefits_until (MAX non-revoked benefit_end), newest first.
func (o *OrdersDB) ListCoupons(ctx context.Context) ([]CouponListItem, error) {
	rows, err := o.Query(ctx,
		`SELECT `+couponColumnsC+`,
			COALESCE(r.cnt, 0), r.benefits_until
		 FROM coupons c
		 LEFT JOIN (
			SELECT coupon_id, COUNT(*) AS cnt,
				MAX(benefit_end) FILTER (WHERE revoked_at IS NULL) AS benefits_until
			FROM coupon_redemptions GROUP BY coupon_id
		 ) r ON r.coupon_id = c.id
		 ORDER BY c.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	items := []CouponListItem{}
	for rows.Next() {
		var it CouponListItem
		c := &it.Coupon
		if err := rows.Scan(&c.ID, &c.Code, &c.Description, &c.Type, &c.Properties, &c.Enabled,
			&c.RedeemFrom, &c.RedeemUntil, &c.BenefitStart, &c.BenefitEnd, &c.BenefitMonths,
			&c.Countries, &c.MaxRedemptions, &c.CreatedAt, &c.UpdatedAt,
			&it.RedemptionsCount, &it.BenefitsUntil); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// UpdateCoupon writes the mutable columns of an existing coupon. The handler is
// responsible for rejecting field changes disallowed by the update rules (§4).
func (o *OrdersDB) UpdateCoupon(ctx context.Context, c Coupon) (*Coupon, error) {
	var out Coupon
	err := scanCoupon(o.QueryRow(ctx,
		`UPDATE coupons SET code=$2, description=$3, type=$4, properties=$5, enabled=$6,
			redeem_from=$7, redeem_until=$8, benefit_start=$9, benefit_end=$10, benefit_months=$11,
			countries=$12, max_redemptions=$13, updated_at=now()
		 WHERE id=$1 RETURNING `+couponColumns,
		c.ID, c.Code, c.Description, c.Type, c.Properties, c.Enabled, c.RedeemFrom, c.RedeemUntil,
		c.BenefitStart, c.BenefitEnd, c.BenefitMonths, c.Countries, c.MaxRedemptions), &out)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, common.ErrNoRowsAffected
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, common.ErrCouponCodeConflict
		}
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}
	return &out, nil
}

func (o *OrdersDB) CountCouponRedemptions(ctx context.Context, couponID int) (int, error) {
	var n int
	if err := o.QueryRow(ctx, `SELECT COUNT(*) FROM coupon_redemptions WHERE coupon_id = $1`, couponID).Scan(&n); err != nil {
		return 0, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}
	return n, nil
}

// ListCouponRedemptions returns the redemptions for a coupon with the member's
// email joined from the orders account.
func (o *OrdersDB) ListCouponRedemptions(ctx context.Context, couponID int) ([]CouponRedemptionDetail, error) {
	rows, err := o.Query(ctx,
		`SELECT r.id, r.coupon_id, r.keycloak_id, r.redeemed_at, r.benefit_start, r.benefit_end,
			r.revoked_at, COALESCE(a."Email", '')
		 FROM coupon_redemptions r
		 LEFT JOIN accounts a ON a."UserKey" = r.keycloak_id
		 WHERE r.coupon_id = $1 ORDER BY r.id DESC`, couponID)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	details := []CouponRedemptionDetail{}
	for rows.Next() {
		var d CouponRedemptionDetail
		if err := rows.Scan(&d.ID, &d.CouponID, &d.KeycloakID, &d.RedeemedAt, &d.BenefitStart,
			&d.BenefitEnd, &d.RevokedAt, &d.Email); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		details = append(details, d)
	}
	return details, rows.Err()
}

// RevokeRedemption soft-revokes a redemption; the row and cap slot remain (§5).
func (o *OrdersDB) RevokeRedemption(ctx context.Context, couponID, redemptionID int) error {
	res, err := o.Exec(ctx,
		`UPDATE coupon_redemptions SET revoked_at = now()
		 WHERE id = $1 AND coupon_id = $2 AND revoked_at IS NULL`, redemptionID, couponID)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}
	return nil
}

// GetMyCoupons returns the caller's non-revoked redemptions on enabled coupons
// whose benefit window has not ended, including not-yet-started mode-A windows.
func (o *OrdersDB) GetMyCoupons(ctx context.Context, keycloakID string) ([]MyCoupon, error) {
	rows, err := o.Query(ctx,
		`SELECT c.code, COALESCE(c.description, ''), r.benefit_start, r.benefit_end
		 FROM coupon_redemptions r JOIN coupons c ON c.id = r.coupon_id
		 WHERE r.keycloak_id = $1 AND r.revoked_at IS NULL AND c.enabled AND now() < r.benefit_end
		 ORDER BY r.benefit_end`, keycloakID)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	mine := []MyCoupon{}
	for rows.Next() {
		var m MyCoupon
		if err := rows.Scan(&m.Code, &m.Description, &m.BenefitStart, &m.BenefitEnd); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		mine = append(mine, m)
	}
	return mine, rows.Err()
}

// GetActiveCouponRedemptions feeds the pricing evaluation: non-revoked redemptions
// currently inside their benefit window on an enabled coupon.
func (o *OrdersDB) GetActiveCouponRedemptions(ctx context.Context, keycloakID string) ([]ActiveCouponRedemption, error) {
	rows, err := o.Query(ctx,
		`SELECT r.id, c.id, c.code, c.type, c.properties, r.benefit_end
		 FROM coupon_redemptions r JOIN coupons c ON c.id = r.coupon_id
		 WHERE r.keycloak_id = $1 AND r.revoked_at IS NULL AND c.enabled
			AND r.benefit_start <= now() AND now() < r.benefit_end`, keycloakID)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	active := []ActiveCouponRedemption{}
	for rows.Next() {
		var a ActiveCouponRedemption
		if err := rows.Scan(&a.RedemptionID, &a.CouponID, &a.Code, &a.Type, &a.Properties, &a.BenefitEnd); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		active = append(active, a)
	}
	return active, rows.Err()
}

// RedeemCoupon runs the redemption flow under a row lock so the cap is race-proof
// (§5). country is the caller's account country ("" if unset). It returns the
// coupon sentinel errors from common for the handler to map to messages.
func (o *OrdersDB) RedeemCoupon(ctx context.Context, keycloakID, code, country string) (*CouponRedemption, error) {
	tx, err := o.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("o.Begin: %w", err)
	}
	defer tx.Rollback(ctx)

	code = strings.TrimSpace(code)
	var c Coupon
	err = scanCoupon(tx.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons WHERE upper(code) = upper($1) FOR UPDATE`, code), &c)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, common.ErrCouponInvalid
		}
		return nil, fmt.Errorf("tx.QueryRow.Scan: %w", err)
	}

	now := time.Now()
	if !c.Enabled || now.Before(c.RedeemFrom) || now.After(c.RedeemUntil) {
		return nil, common.ErrCouponInvalid
	}
	if !countryAllowed(c.Countries, country) {
		if country == "" {
			return nil, common.ErrCouponCountryRequired
		}
		return nil, common.ErrCouponCountryMismatch
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM coupon_redemptions WHERE coupon_id = $1`, c.ID).Scan(&count); err != nil {
		return nil, fmt.Errorf("tx.QueryRow count: %w", err)
	}
	if count >= c.MaxRedemptions {
		return nil, common.ErrCouponExhausted
	}

	start, end, err := coupon.BenefitWindow(c.BenefitMonths.Ptr(), timePtr(c.BenefitStart), timePtr(c.BenefitEnd), now)
	if err != nil {
		return nil, fmt.Errorf("coupon.BenefitWindow: %w", err)
	}

	r := CouponRedemption{CouponID: c.ID, KeycloakID: keycloakID}
	err = tx.QueryRow(ctx,
		`INSERT INTO coupon_redemptions (coupon_id, keycloak_id, benefit_start, benefit_end)
		 VALUES ($1,$2,$3,$4) RETURNING id, redeemed_at, benefit_start, benefit_end`,
		c.ID, keycloakID, start, end,
	).Scan(&r.ID, &r.RedeemedAt, &r.BenefitStart, &r.BenefitEnd)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, common.ErrCouponAlreadyRedeemed
		}
		return nil, fmt.Errorf("tx.QueryRow insert: %w", err)
	}

	return &r, tx.Commit(ctx)
}

func countryAllowed(countries []string, country string) bool {
	if len(countries) == 0 {
		return true // unrestricted
	}
	for _, c := range countries {
		if strings.EqualFold(c, country) {
			return true
		}
	}
	return false
}

func timePtr(t null.Time) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
