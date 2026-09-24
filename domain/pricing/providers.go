package pricing

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// The discount lookups pricing needs from storage. They were already one
// method each; they lived in repo, with doc comments naming this package as
// their consumer. *repo.OrdersDB satisfies all three implicitly.
type (
	// ManualDiscountProvider fetches the active manual discount for a user.
	ManualDiscountProvider interface {
		GetActiveManualDiscount(ctx context.Context, keycloakID string) (*repo.ManualDiscount, error)
	}

	// HHGrantProvider fetches the active Help Haver grant for a user.
	HHGrantProvider interface {
		GetActiveHHGrant(ctx context.Context, keycloakID string) (*repo.HHGrant, error)
	}

	// CouponProvider fetches a member's active coupon redemptions for pricing.
	CouponProvider interface {
		GetActiveCouponRedemptions(ctx context.Context, keycloakID string) ([]repo.ActiveCouponRedemption, error)
	}
)
