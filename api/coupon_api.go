package api

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// couponRepo is what the /v2/coupon handlers call: nine coupon methods plus
// the two account reads that resolve a redeemer's country. Unexported so it
// stays this package's contract and not the next shared one.
type couponRepo interface {
	CreateCoupon(ctx context.Context, c repo.Coupon) (*repo.Coupon, error)
	GetCouponByID(ctx context.Context, id int) (*repo.Coupon, error)
	ListCoupons(ctx context.Context) ([]repo.CouponListItem, error)
	UpdateCoupon(ctx context.Context, c repo.Coupon) (*repo.Coupon, error)
	CountCouponRedemptions(ctx context.Context, couponID int) (int, error)
	ListCouponRedemptions(ctx context.Context, couponID int) ([]repo.CouponRedemptionDetail, error)
	RevokeRedemption(ctx context.Context, couponID, redemptionID int) error
	GetMyCoupons(ctx context.Context, keycloakID string) ([]repo.MyCoupon, error)
	RedeemCoupon(ctx context.Context, keycloakID, code, country string) (*repo.CouponRedemption, error)

	GetAccountIDByKeycloakID(ctx context.Context, keycloakId string) (int, error)
	GetAccount(ctx context.Context, id int, email string) (*repo.Account, error)
}

// CouponAPI serves the /v2/coupon endpoints. One handler group, one
// dependency — unlike *OrdersAPI, where every handler can reach the repo,
// Pelecard, Priority and the accounting service alike.
type CouponAPI struct {
	repo couponRepo
}

func NewCouponAPI(db couponRepo) *CouponAPI {
	return &CouponAPI{repo: db}
}
