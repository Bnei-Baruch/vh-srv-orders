package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func percentCoupon(code string, months, max int) Coupon {
	pct := 50.0
	props, _ := json.Marshal(CouponProperties{DiscountPct: &pct})
	return Coupon{
		Code:           code,
		Type:           "percent",
		Properties:     null.JSONFrom(props),
		Enabled:        true,
		RedeemFrom:     time.Now().Add(-time.Hour),
		RedeemUntil:    time.Now().Add(30 * 24 * time.Hour),
		BenefitMonths:  null.IntFrom(months),
		MaxRedemptions: max,
	}
}

func mustCreate(t *testing.T, db *OrdersDB, ctx context.Context, c Coupon) *Coupon {
	t.Helper()
	created, err := db.CreateCoupon(ctx, c)
	require.NoError(t, err)
	return created
}

// insertRedemption inserts a redemption directly with an explicit window so the
// pricing/visibility filters can be exercised precisely.
func insertRedemption(t *testing.T, db *OrdersDB, ctx context.Context, couponID int, kc string, startDaysAgo, endInDays int, revoked bool) int {
	t.Helper()
	revokedAt := "NULL"
	if revoked {
		revokedAt = "now()"
	}
	var id int
	err := db.QueryRow(ctx,
		`INSERT INTO coupon_redemptions (coupon_id, keycloak_id, benefit_start, benefit_end, revoked_at)
		 VALUES ($1, $2, now() - ($3 * interval '1 day'), now() + ($4 * interval '1 day'), `+revokedAt+`)
		 RETURNING id`, couponID, kc, startDaysAgo, endInDays).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestCreateAndGetCoupon_Roundtrip(t *testing.T) {
	db, ctx := newTestDB(t)
	created := mustCreate(t, db, ctx, percentCoupon("SUKKOT-AAAAA", 3, 25))

	got, err := db.GetCouponByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "SUKKOT-AAAAA", got.Code)
	assert.Equal(t, "percent", got.Type)
	assert.True(t, got.Enabled)
	assert.Equal(t, 3, got.BenefitMonths.Int)
	assert.Equal(t, 25, got.MaxRedemptions)
}

func TestGetCouponByID_Unknown_ReturnsErrNoRowsAffected(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.GetCouponByID(ctx, 99999)
	assert.ErrorIs(t, err, common.ErrNoRowsAffected)
}

func TestListCoupons_CountsAndBenefitsUntil(t *testing.T) {
	db, ctx := newTestDB(t)
	c := mustCreate(t, db, ctx, percentCoupon("A-AAAAA", 3, 10))
	insertRedemption(t, db, ctx, c.ID, "kc-1", 1, 30, false)
	insertRedemption(t, db, ctx, c.ID, "kc-2", 1, 60, false)
	insertRedemption(t, db, ctx, c.ID, "kc-3", 1, 90, true) // revoked, still counted

	items, err := db.ListCoupons(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 3, items[0].RedemptionsCount, "count includes revoked rows")
	require.True(t, items[0].BenefitsUntil.Valid)
	// benefits_until is the MAX over non-revoked rows (~60 days out), not the revoked 90.
	assert.True(t, items[0].BenefitsUntil.Time.Before(time.Now().Add(75*24*time.Hour)))
}

func TestRedeemCoupon_HappyPath_ComputesOffsetWindow(t *testing.T) {
	db, ctx := newTestDB(t)
	c := mustCreate(t, db, ctx, percentCoupon("SUKKOT-BBBBB", 3, 10))

	r, err := db.RedeemCoupon(ctx, "kc-redeemer", "sukkot-bbbbb", "IL") // lower-case input normalized
	require.NoError(t, err)
	assert.Equal(t, c.ID, r.CouponID)
	assert.Equal(t, "kc-redeemer", r.KeycloakID)

	// Offset window colors whole calendar months in the billing timezone: both
	// ends are midnight-Jerusalem first-of-month, exactly 3 months apart.
	loc, err := time.LoadLocation("Asia/Jerusalem")
	require.NoError(t, err)
	bs, be := r.BenefitStart.In(loc), r.BenefitEnd.In(loc)
	assert.Equal(t, 1, bs.Day())
	assert.Equal(t, 0, bs.Hour())
	assert.Equal(t, 1, be.Day())
	assert.Equal(t, 3, (be.Year()*12+int(be.Month()))-(bs.Year()*12+int(bs.Month())), "3 calendar months")
}

func TestRedeemCoupon_AlreadyRedeemed(t *testing.T) {
	db, ctx := newTestDB(t)
	mustCreate(t, db, ctx, percentCoupon("ONCE-AAAAA", 3, 10))

	_, err := db.RedeemCoupon(ctx, "kc-1", "ONCE-AAAAA", "IL")
	require.NoError(t, err)
	_, err = db.RedeemCoupon(ctx, "kc-1", "ONCE-AAAAA", "IL")
	assert.ErrorIs(t, err, common.ErrCouponAlreadyRedeemed)
}

func TestRedeemCoupon_CapExhausted(t *testing.T) {
	db, ctx := newTestDB(t)
	mustCreate(t, db, ctx, percentCoupon("CAP-AAAAA", 3, 1))

	_, err := db.RedeemCoupon(ctx, "kc-1", "CAP-AAAAA", "IL")
	require.NoError(t, err)
	_, err = db.RedeemCoupon(ctx, "kc-2", "CAP-AAAAA", "IL")
	assert.ErrorIs(t, err, common.ErrCouponExhausted)
}

func TestRedeemCoupon_Disabled_Invalid(t *testing.T) {
	db, ctx := newTestDB(t)
	c := percentCoupon("OFF-AAAAA", 3, 10)
	c.Enabled = false
	mustCreate(t, db, ctx, c)

	_, err := db.RedeemCoupon(ctx, "kc-1", "OFF-AAAAA", "IL")
	assert.ErrorIs(t, err, common.ErrCouponInvalid)
}

func TestRedeemCoupon_OutOfWindow_Invalid(t *testing.T) {
	db, ctx := newTestDB(t)
	c := percentCoupon("EXP-AAAAA", 3, 10)
	c.RedeemFrom = time.Now().Add(-48 * time.Hour)
	c.RedeemUntil = time.Now().Add(-24 * time.Hour) // already closed
	mustCreate(t, db, ctx, c)

	_, err := db.RedeemCoupon(ctx, "kc-1", "EXP-AAAAA", "IL")
	assert.ErrorIs(t, err, common.ErrCouponInvalid)
}

func TestRedeemCoupon_CountryScope(t *testing.T) {
	db, ctx := newTestDB(t)
	c := percentCoupon("GEO-AAAAA", 3, 10)
	c.Countries = []string{"IL"}
	mustCreate(t, db, ctx, c)

	_, err := db.RedeemCoupon(ctx, "kc-wrong", "GEO-AAAAA", "US")
	assert.ErrorIs(t, err, common.ErrCouponCountryMismatch)

	_, err = db.RedeemCoupon(ctx, "kc-none", "GEO-AAAAA", "")
	assert.ErrorIs(t, err, common.ErrCouponCountryRequired)

	r, err := db.RedeemCoupon(ctx, "kc-il", "GEO-AAAAA", "IL")
	require.NoError(t, err)
	assert.Greater(t, r.ID, 0)
}

func TestGetActiveCouponRedemptions_WindowAndStateFilters(t *testing.T) {
	db, ctx := newTestDB(t)
	active := mustCreate(t, db, ctx, percentCoupon("ACT-AAAAA", 3, 100))
	disabled := percentCoupon("DIS-AAAAA", 3, 100)
	disabled.Enabled = false
	disabledC := mustCreate(t, db, ctx, disabled)

	kc := "kc-member"
	insertRedemption(t, db, ctx, active.ID, kc, 1, 30, false)      // active now → included
	insertRedemption(t, db, ctx, active.ID, kc+"-x", 1, 30, false) // other member, ignored by kc filter
	future := active.ID
	// not-yet-started: benefit_start in the future
	_, err := db.Exec(ctx,
		`INSERT INTO coupon_redemptions (coupon_id, keycloak_id, benefit_start, benefit_end)
		 VALUES ($1, $2, now() + interval '5 days', now() + interval '40 days')`, future, kc+"-future")
	require.NoError(t, err)
	insertRedemption(t, db, ctx, active.ID, kc+"-expired", 40, -1, false) // ended → excluded
	insertRedemption(t, db, ctx, active.ID, kc+"-revoked", 1, 30, true)   // revoked → excluded
	insertRedemption(t, db, ctx, disabledC.ID, kc, 1, 30, false)          // coupon disabled → excluded

	got, err := db.GetActiveCouponRedemptions(ctx, kc)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the active, in-window, non-revoked redemption on an enabled coupon")
	assert.Equal(t, active.ID, got[0].CouponID)
}

func TestRevokeRedemption_ExcludesFromActiveButKeepsCount(t *testing.T) {
	db, ctx := newTestDB(t)
	c := mustCreate(t, db, ctx, percentCoupon("REV-AAAAA", 3, 100))
	rid := insertRedemption(t, db, ctx, c.ID, "kc-1", 1, 30, false)

	require.NoError(t, db.RevokeRedemption(ctx, c.ID, rid))

	active, err := db.GetActiveCouponRedemptions(ctx, "kc-1")
	require.NoError(t, err)
	assert.Empty(t, active, "revoked redemption drops out of pricing")

	count, err := db.CountCouponRedemptions(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "revoked row still counts toward the cap")

	// Revoking again is a no-op.
	assert.ErrorIs(t, db.RevokeRedemption(ctx, c.ID, rid), common.ErrNoRowsAffected)
}

func TestGetMyCoupons_IncludesActiveExcludesEndedRevokedDisabled(t *testing.T) {
	db, ctx := newTestDB(t)
	kc := "kc-me"

	activeC := mustCreate(t, db, ctx, percentCoupon("MINE-ACTIV", 3, 100))
	endedC := mustCreate(t, db, ctx, percentCoupon("MINE-ENDED", 3, 100))
	revokedC := mustCreate(t, db, ctx, percentCoupon("MINE-REVOK", 3, 100))
	disabled := percentCoupon("MINE-DISAB", 3, 100)
	disabled.Enabled = false
	disabledC := mustCreate(t, db, ctx, disabled)

	insertRedemption(t, db, ctx, activeC.ID, kc, 1, 30, false)   // active → included
	insertRedemption(t, db, ctx, endedC.ID, kc, 40, -1, false)   // ended → excluded
	insertRedemption(t, db, ctx, revokedC.ID, kc, 1, 30, true)   // revoked → excluded
	insertRedemption(t, db, ctx, disabledC.ID, kc, 1, 30, false) // coupon disabled → excluded

	mine, err := db.GetMyCoupons(ctx, kc)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "MINE-ACTIV", mine[0].Code)
	assert.True(t, time.Now().Before(mine[0].BenefitEnd))
}

func TestUpdateCoupon_Roundtrip(t *testing.T) {
	db, ctx := newTestDB(t)
	c := mustCreate(t, db, ctx, percentCoupon("UPD-AAAAA", 3, 10))

	c.Enabled = false
	c.MaxRedemptions = 50
	c.Description = null.StringFrom("seasonal")
	updated, err := db.UpdateCoupon(ctx, *c)
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
	assert.Equal(t, 50, updated.MaxRedemptions)
	assert.Equal(t, "seasonal", updated.Description.String)
}
