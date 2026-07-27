package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/coupon"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// Coupon dates cross the API as inclusive "YYYY-MM-DD" calendar days in the
// billing timezone (Asia/Jerusalem). The backend owns all timezone conversion
// so the BO never does date math — it just passes the admin's raw dates.

// couponDayToTime parses an admin date in Asia/Jerusalem. inclusiveEnd shifts an
// inclusive last-covered day to the exclusive next-day-midnight boundary the
// pricing/redemption checks compare against (now < benefit_end, now <= redeem_until).
func couponDayToTime(s string, inclusiveEnd bool) (time.Time, error) {
	loc, err := time.LoadLocation(coupon.JerusalemTZ)
	if err != nil {
		return time.Time{}, err
	}
	d, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", s)
	}
	if inclusiveEnd {
		d = d.AddDate(0, 0, 1)
	}
	return d, nil
}

// couponTimeToDay renders a stored timestamp back as its inclusive Jerusalem
// calendar day for the admin UI. inclusiveEnd undoes the +1 applied on save.
func couponTimeToDay(t time.Time, inclusiveEnd bool) string {
	loc, err := time.LoadLocation(coupon.JerusalemTZ)
	if err != nil {
		return ""
	}
	d := t.In(loc)
	if inclusiveEnd {
		d = d.AddDate(0, 0, -1)
	}
	return d.Format("2006-01-02")
}

func couponNullTimeToDay(t null.Time, inclusiveEnd bool) string {
	if !t.Valid {
		return ""
	}
	return couponTimeToDay(t.Time, inclusiveEnd)
}

// couponDayNullTime parses an optional admin date into a null.Time ("" → null).
func couponDayNullTime(s string, inclusiveEnd bool) (null.Time, error) {
	if s == "" {
		return null.Time{}, nil
	}
	t, err := couponDayToTime(s, inclusiveEnd)
	if err != nil {
		return null.Time{}, err
	}
	return null.TimeFrom(t), nil
}

type couponCreateReq struct {
	Prefix         string    `json:"prefix" binding:"required"`
	Description    *string   `json:"description"`
	Type           string    `json:"type" binding:"required"`
	Properties     null.JSON `json:"properties"`
	RedeemFrom     string    `json:"redeem_from"`   // YYYY-MM-DD, Asia/Jerusalem (empty = now)
	RedeemUntil    string    `json:"redeem_until"`  // YYYY-MM-DD (empty = redeem_from + 30d)
	BenefitStart   string    `json:"benefit_start"` // YYYY-MM-DD (mode A)
	BenefitEnd     string    `json:"benefit_end"`   // YYYY-MM-DD, inclusive last covered day
	BenefitMonths  *int      `json:"benefit_months"`
	Countries      []string  `json:"countries"`
	MaxRedemptions int       `json:"max_redemptions" binding:"required"`
}

func (o *OrdersAPI) handleCreateCoupon(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}

	var req couponCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	redeemFrom := time.Now()
	if req.RedeemFrom != "" {
		t, err := couponDayToTime(req.RedeemFrom, false)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		redeemFrom = t
	}
	redeemUntil := redeemFrom.AddDate(0, 0, coupon.DefaultRedeemDays)
	if req.RedeemUntil != "" {
		t, err := couponDayToTime(req.RedeemUntil, true)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		redeemUntil = t
	}

	benefitStart, err := couponDayNullTime(req.BenefitStart, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	benefitEnd, err := couponDayNullTime(req.BenefitEnd, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	newCoupon := repo.Coupon{
		Description:    null.StringFromPtr(req.Description),
		Type:           req.Type,
		Properties:     req.Properties,
		Enabled:        true,
		RedeemFrom:     redeemFrom,
		RedeemUntil:    redeemUntil,
		BenefitStart:   benefitStart,
		BenefitEnd:     benefitEnd,
		BenefitMonths:  null.IntFromPtr(req.BenefitMonths),
		Countries:      req.Countries,
		MaxRedemptions: req.MaxRedemptions,
	}

	if err := validateCoupon(newCoupon); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	code, err := coupon.GenerateCode(req.Prefix)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	newCoupon.Code = code

	created, err := o.repo.CreateCoupon(c.Request.Context(), newCoupon)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CreateCoupon: %w", err))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": created, "success": true})
}

// couponResponse augments a coupon-list row with status labels and renders all
// dates as inclusive Jerusalem "YYYY-MM-DD" strings (shadowing the embedded
// time.Time fields) so the BO does no timezone math.
type couponResponse struct {
	repo.CouponListItem
	Status        []string `json:"status"`
	RedeemFrom    string   `json:"redeem_from"`
	RedeemUntil   string   `json:"redeem_until"`
	BenefitStart  string   `json:"benefit_start"`
	BenefitEnd    string   `json:"benefit_end"`
	BenefitsUntil string   `json:"benefits_until"`
}

func newCouponResponse(it repo.CouponListItem) couponResponse {
	return couponResponse{
		CouponListItem: it,
		Status:         couponStatus(it),
		RedeemFrom:     couponTimeToDay(it.RedeemFrom, false),
		RedeemUntil:    couponTimeToDay(it.RedeemUntil, true),
		BenefitStart:   couponNullTimeToDay(it.BenefitStart, false),
		BenefitEnd:     couponNullTimeToDay(it.BenefitEnd, true),
		BenefitsUntil:  couponNullTimeToDay(it.BenefitsUntil, true),
	}
}

func couponStatus(it repo.CouponListItem) []string {
	var labels []string
	if !it.Enabled {
		labels = append(labels, "disabled")
	}
	if time.Now().After(it.RedeemUntil) {
		labels = append(labels, "redemption_closed")
	}
	if it.RedemptionsCount >= it.MaxRedemptions {
		labels = append(labels, "exhausted")
	}
	if len(labels) == 0 {
		labels = append(labels, "active")
	}
	return labels
}

func (o *OrdersAPI) handleListCoupons(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	items, err := o.repo.ListCoupons(c.Request.Context())
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.ListCoupons: %w", err))
		return
	}
	resp := make([]couponResponse, len(items))
	for i, it := range items {
		resp[i] = newCouponResponse(it)
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": resp, "success": true})
}

// couponRedemptionResponse renders redemption dates as inclusive Jerusalem days.
type couponRedemptionResponse struct {
	repo.CouponRedemptionDetail
	RedeemedAt   string `json:"redeemed_at"`
	BenefitStart string `json:"benefit_start"`
	BenefitEnd   string `json:"benefit_end"`
}

func (o *OrdersAPI) handleGetCouponRedemptions(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}
	details, err := o.repo.ListCouponRedemptions(c.Request.Context(), id)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.ListCouponRedemptions: %w", err))
		return
	}
	resp := make([]couponRedemptionResponse, len(details))
	for i, d := range details {
		resp[i] = couponRedemptionResponse{
			CouponRedemptionDetail: d,
			RedeemedAt:             couponTimeToDay(d.RedeemedAt, false),
			BenefitStart:           couponTimeToDay(d.BenefitStart, false),
			BenefitEnd:             couponTimeToDay(d.BenefitEnd, true),
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": resp, "success": true})
}

type couponUpdateReq struct {
	Description    *string          `json:"description"`
	Enabled        *bool            `json:"enabled"`
	Countries      *[]string        `json:"countries"`
	RedeemUntil    *string          `json:"redeem_until"` // YYYY-MM-DD, Asia/Jerusalem
	MaxRedemptions *int             `json:"max_redemptions"`
	Code           *string          `json:"code"`
	Type           *string          `json:"type"`
	Properties     *json.RawMessage `json:"properties"`
	BenefitStart   *string          `json:"benefit_start"` // YYYY-MM-DD
	BenefitEnd     *string          `json:"benefit_end"`   // YYYY-MM-DD, inclusive last covered day
	BenefitMonths  *int             `json:"benefit_months"`
}

func (o *OrdersAPI) handleUpdateCoupon(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}
	var req couponUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := o.repo.GetCouponByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetCouponByID: %w", err))
		}
		return
	}
	count, err := o.repo.CountCouponRedemptions(c.Request.Context(), id)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CountCouponRedemptions: %w", err))
		return
	}

	// Fields locked once the coupon has redemptions (§4): discount, benefit, code.
	locksTouched := req.Code != nil || req.Type != nil || req.Properties != nil ||
		req.BenefitStart != nil || req.BenefitEnd != nil || req.BenefitMonths != nil
	if count > 0 && locksTouched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "discount, benefit, and code cannot change after the coupon has redemptions"})
		return
	}

	if err := applyCouponUpdate(existing, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := validateCoupon(*existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updated, err := o.repo.UpdateCoupon(c.Request.Context(), *existing)
	if err != nil {
		if errors.Is(err, common.ErrCouponCodeConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.UpdateCoupon: %w", err))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Updated!", "data": updated, "success": true})
}

func applyCouponUpdate(c *repo.Coupon, req couponUpdateReq) error {
	if req.Description != nil {
		c.Description = null.StringFrom(*req.Description)
	}
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	if req.Countries != nil {
		c.Countries = *req.Countries
	}
	if req.RedeemUntil != nil {
		t, err := couponDayToTime(*req.RedeemUntil, true)
		if err != nil {
			return err
		}
		c.RedeemUntil = t
	}
	if req.MaxRedemptions != nil {
		c.MaxRedemptions = *req.MaxRedemptions
	}
	if req.Code != nil {
		c.Code = strings.ToUpper(strings.TrimSpace(*req.Code))
	}
	if req.Type != nil {
		c.Type = *req.Type
	}
	if req.Properties != nil {
		c.Properties = null.JSONFrom(*req.Properties)
	}
	// The benefit definition is replaced as a unit: if any benefit field is
	// present, set the chosen mode and clear the other, so switching modes never
	// leaves both populated (which would fail "exactly one benefit mode").
	if req.BenefitMonths != nil || req.BenefitStart != nil || req.BenefitEnd != nil {
		start, err := couponDayNullTime(strPtrVal(req.BenefitStart), false)
		if err != nil {
			return err
		}
		end, err := couponDayNullTime(strPtrVal(req.BenefitEnd), true)
		if err != nil {
			return err
		}
		c.BenefitMonths = null.IntFromPtr(req.BenefitMonths)
		c.BenefitStart = start
		c.BenefitEnd = end
	}
	return nil
}

func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (o *OrdersAPI) handleRevokeRedemption(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	couponID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}
	redemptionID, err := strconv.Atoi(c.Param("rid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rid! Accepted value is INTEGER", "success": false})
		return
	}
	if err := o.repo.RevokeRedemption(c.Request.Context(), couponID, redemptionID); err != nil {
		if errors.Is(err, common.ErrNoRowsAffected) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.RevokeRedemption: %w", err))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Revoked!", "success": true})
}

// myCouponResponse renders a member's coupon dates as inclusive Jerusalem days
// so vh-dash displays them without any timezone math.
type myCouponResponse struct {
	repo.MyCoupon
	BenefitStart string `json:"benefit_start"`
	BenefitEnd   string `json:"benefit_end"` // inclusive last covered day
}

func (o *OrdersAPI) handleGetMyCoupons(c *gin.Context) {
	keycloakID, ok := o.getUserKeyFromRequest(c)
	if !ok {
		return
	}
	mine, err := o.repo.GetMyCoupons(c.Request.Context(), keycloakID)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetMyCoupons: %w", err))
		return
	}
	resp := make([]myCouponResponse, len(mine))
	for i, m := range mine {
		resp[i] = myCouponResponse{
			MyCoupon:     m,
			BenefitStart: couponTimeToDay(m.BenefitStart, false),
			BenefitEnd:   couponTimeToDay(m.BenefitEnd, true),
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": resp, "success": true})
}

func (o *OrdersAPI) handleRedeemCoupon(c *gin.Context) {
	keycloakID, ok := o.getUserKeyFromRequest(c)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	country := o.accountCountry(c, keycloakID)

	redemption, err := o.repo.RedeemCoupon(c.Request.Context(), keycloakID, body.Code, country)
	if err != nil {
		switch {
		case errors.Is(err, common.ErrCouponInvalid),
			errors.Is(err, common.ErrCouponCountryMismatch),
			errors.Is(err, common.ErrCouponCountryRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, common.ErrCouponAlreadyRedeemed),
			errors.Is(err, common.ErrCouponExhausted):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.RedeemCoupon: %w", err))
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Redeemed!", "data": redemption, "success": true})
}

// accountCountry resolves the caller's account country, or "" if there is no
// account yet (a country-scoped coupon then surfaces "set your country first").
func (o *OrdersAPI) accountCountry(c *gin.Context, keycloakID string) string {
	accID, err := o.repo.GetAccountIDByKeycloakID(c.Request.Context(), keycloakID)
	if err != nil {
		return ""
	}
	acc, err := o.repo.GetAccount(c.Request.Context(), accID, "")
	if err != nil {
		return ""
	}
	return acc.Country.String
}

// validateCoupon runs the pure domain validation (design §1) on a repo coupon.
func validateCoupon(c repo.Coupon) error {
	var p repo.CouponProperties
	if c.Properties.Valid && len(c.Properties.JSON) > 0 {
		if err := json.Unmarshal(c.Properties.JSON, &p); err != nil {
			return fmt.Errorf("invalid properties JSON: %w", err)
		}
	}
	return coupon.Validate(coupon.Fields{
		Type:           c.Type,
		DiscountPct:    p.DiscountPct,
		FixedPrice:     p.FixedPrice,
		FixedCurrency:  p.Currency,
		RedeemFrom:     c.RedeemFrom,
		RedeemUntil:    c.RedeemUntil,
		BenefitStart:   c.BenefitStart.Ptr(),
		BenefitEnd:     c.BenefitEnd.Ptr(),
		BenefitMonths:  c.BenefitMonths.Ptr(),
		Countries:      c.Countries,
		MaxRedemptions: c.MaxRedemptions,
	})
}
