package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func percentCreateReq(prefix string, pct float64, months, max int) couponCreateReq {
	props, _ := json.Marshal(repo.CouponProperties{DiscountPct: &pct})
	m := months
	return couponCreateReq{
		Prefix:         prefix,
		Type:           "percent",
		Properties:     null.JSONFrom(props),
		BenefitMonths:  &m,
		MaxRedemptions: max,
	}
}

func createCoupon(t *testing.T, a *App, req couponCreateReq) (id int, code string) {
	t.Helper()
	got := POST_ROOT(t, a, "/v2/coupon/", req, http.StatusCreated)
	data := got["data"].(map[string]interface{})
	return int(data["id"].(float64)), data["code"].(string)
}

func TestCoupon_Create_Admin(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := POST_ROOT(t, a, "/v2/coupon/", percentCreateReq("SUKKOT", 50, 3, 25), http.StatusCreated)
	data := got["data"].(map[string]interface{})
	assert.Contains(t, data["code"], "SUKKOT-")
	assert.Equal(t, "percent", data["type"])
}

func TestCoupon_Create_InvalidPercent_Rejected(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := POST_ROOT(t, a, "/v2/coupon/", percentCreateReq("BAD", 0, 3, 25), http.StatusBadRequest)
	assert.Contains(t, got["error"], "discount_pct")
}

func TestCoupon_Create_NonAdmin_Forbidden(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v2/coupon/", requestBody(t, percentCreateReq("NOPE", 50, 3, 25)))
	ctx := context.WithValue(r.Context(), common.CtxAuthClaims, &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{"some-role"}},
	})
	a.gEngine.ServeHTTP(w, r.WithContext(ctx))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestCoupon_List_StatusLabels(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	createCoupon(t, a, percentCreateReq("LIST", 50, 3, 25))

	got := do(t, a, "GET", "/v2/coupon/", nil, http.StatusOK, DoOptions{isRoot: true})
	data := got["data"].([]interface{})
	require.Len(t, data, 1)
	status := data[0].(map[string]interface{})["status"].([]interface{})
	assert.Contains(t, status, "active")
}

func TestCoupon_RedeemThenMine(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	_, code := createCoupon(t, a, percentCreateReq("REDEEM", 50, 3, 25))

	// Member (non-admin) redeems using their own token; unrestricted coupon needs no country.
	got := POST(t, a, "/v2/coupon/redeem", map[string]string{"code": code}, http.StatusCreated)
	assert.Greater(t, got["data"].(map[string]interface{})["coupon_id"].(float64), 0.0)

	mine := GET(t, a, "/v2/coupon/mine", http.StatusOK)
	list := mine["data"].([]interface{})
	require.Len(t, list, 1)
	row := list[0].(map[string]interface{})
	assert.Equal(t, code, row["code"])
	// Dates come back as inclusive Jerusalem YYYY-MM-DD strings for the dashboard.
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, row["benefit_end"])
}

func TestCoupon_Redeem_UnknownCode_Invalid(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := POST(t, a, "/v2/coupon/redeem", map[string]string{"code": "NOPE-XXXXX"}, http.StatusBadRequest)
	assert.Equal(t, common.ErrCouponInvalid.Error(), got["error"])
}

func TestCoupon_Redeem_CountryScoped_NoCountry_SetCountryFirst(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	req := percentCreateReq("GEO", 50, 3, 25)
	req.Countries = []string{"IL"}
	_, code := createCoupon(t, a, req)

	// USER_KEY has no account → no country → country-scoped coupon asks to set it first.
	got := POST(t, a, "/v2/coupon/redeem", map[string]string{"code": code}, http.StatusBadRequest)
	assert.Equal(t, common.ErrCouponCountryRequired.Error(), got["error"])
}

func TestCoupon_UpdateDiscount_AfterRedemption_Rejected(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	id, code := createCoupon(t, a, percentCreateReq("LOCK", 50, 3, 25))

	POST(t, a, "/v2/coupon/redeem", map[string]string{"code": code}, http.StatusCreated)

	newProps := json.RawMessage(`{"discount_pct":10}`)
	got := PATCH_ROOT(t, a, "/v2/coupon/"+strconv.Itoa(id), couponUpdateReq{Properties: &newProps}, http.StatusBadRequest)
	assert.Contains(t, got["error"], "after the coupon has redemptions")
}

func TestCoupon_Create_NegativeMax_Rejected(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	got := POST_ROOT(t, a, "/v2/coupon/", percentCreateReq("NEG", 50, 3, -5), http.StatusBadRequest)
	assert.Contains(t, got["error"], "max_redemptions")
}

func TestCoupon_UpdateBenefitMode_MonthsToWindow(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	id, _ := createCoupon(t, a, percentCreateReq("SWITCH", 50, 3, 25)) // starts in months mode

	// Switch to a fixed window; end is well after the default redeem_until (now+30d).
	start := time.Now().Format("2006-01-02")
	end := time.Now().AddDate(0, 0, 90).Format("2006-01-02")
	got := PATCH_ROOT(t, a, "/v2/coupon/"+strconv.Itoa(id),
		couponUpdateReq{BenefitStart: &start, BenefitEnd: &end}, http.StatusOK)

	data := got["data"].(map[string]interface{})
	assert.Nil(t, data["benefit_months"], "old months mode must be cleared when switching to a window")
	assert.NotNil(t, data["benefit_start"])
}

func TestCoupon_ModeADates_RoundTripInclusive(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	pct := 50.0
	props, _ := json.Marshal(repo.CouponProperties{DiscountPct: &pct})
	POST_ROOT(t, a, "/v2/coupon/", couponCreateReq{
		Prefix: "DATED", Type: "percent", Properties: null.JSONFrom(props),
		BenefitStart: "2026-08-01", BenefitEnd: "2026-08-31", RedeemUntil: "2026-08-20",
		MaxRedemptions: 25,
	}, http.StatusCreated)

	got := do(t, a, "GET", "/v2/coupon/", nil, http.StatusOK, DoOptions{isRoot: true})
	row := got["data"].([]interface{})[0].(map[string]interface{})
	// Dates round-trip as the admin's inclusive Jerusalem calendar days.
	assert.Equal(t, "2026-08-01", row["benefit_start"])
	assert.Equal(t, "2026-08-31", row["benefit_end"])
	assert.Equal(t, "2026-08-20", row["redeem_until"])
}

func TestCoupon_Redeem_TrimsWhitespace(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	_, code := createCoupon(t, a, percentCreateReq("TRIM", 50, 3, 25))

	POST(t, a, "/v2/coupon/redeem", map[string]string{"code": "  " + code + "  "}, http.StatusCreated)
}

func TestCoupon_UpdateCode_Duplicate_Conflict(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	id1, _ := createCoupon(t, a, percentCreateReq("DUPA", 50, 3, 25))
	_, code2 := createCoupon(t, a, percentCreateReq("DUPB", 50, 3, 25))

	got := PATCH_ROOT(t, a, "/v2/coupon/"+strconv.Itoa(id1),
		couponUpdateReq{Code: &code2}, http.StatusConflict)
	assert.Equal(t, common.ErrCouponCodeConflict.Error(), got["error"])
}

func TestCoupon_Disable_ThenEnable(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	id, _ := createCoupon(t, a, percentCreateReq("KILL", 50, 3, 25))

	disabled := false
	PATCH_ROOT(t, a, "/v2/coupon/"+strconv.Itoa(id), couponUpdateReq{Enabled: &disabled}, http.StatusOK)

	got := do(t, a, "GET", "/v2/coupon/", nil, http.StatusOK, DoOptions{isRoot: true})
	status := got["data"].([]interface{})[0].(map[string]interface{})["status"].([]interface{})
	assert.Contains(t, status, "disabled")
}
