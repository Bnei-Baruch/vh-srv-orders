package api

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// couponRepo is the storage the coupon endpoints use: nine coupon calls plus
// the two account reads that resolve the caller's country for a redemption.
//
// It is unexported on purpose. Nothing outside api needs to name it, and an
// unexported interface cannot become the next shared contract that everything
// takes whole — which is how OrdersRepository grew to a hundred methods.
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

// CouponAPI serves the /v2/coupon endpoints.
//
// One handler group, one struct, one dependency — the eleven calls above.
// OrdersAPI carries five collaborators for its ~60 repo calls, and every
// handler on it can reach all of them: a coupon handler can call the Pelecard
// client or charge a card, and nothing in the type says otherwise. Here the
// reachable set is the interface, so the compiler states the blast radius of a
// coupon change instead of a reviewer having to.
//
// The handlers themselves are unchanged; only their receiver is. That is the
// point — peeling a group off OrdersAPI is a receiver swap and a route line,
// not a rewrite, and the integration tests for these endpoints did not change
// at all.
type CouponAPI struct {
	repo couponRepo
}

func NewCouponAPI(db couponRepo) *CouponAPI {
	return &CouponAPI{repo: db}
}
