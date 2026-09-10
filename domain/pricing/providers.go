package pricing

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// The three discount lookups pricing needs from storage, one method each.
//
// They were already this narrow — they just lived in repo, next to the
// implementation, with doc comments pointing at their single consumer
// ("injected into the pricing resolver"). A producer that names its consumer
// in a comment is describing an interface that belongs to the consumer.
//
// Moving them here changes who may break whom. In repo they were part of
// repo's public surface, so any package could depend on them and repo had to
// keep them stable for all of it; a pricing change meant editing a file three
// directories away, under the invariant that repo/ is data access only.
// Declared here, they say what this package needs, and they move when it does.
//
// *repo.OrdersDB still satisfies all three implicitly. cmd/billing.go passes
// it unchanged, and the api handlers pass their repo.OrdersRepository into
// GetMonthlyPrice unchanged — a wider interface value converts to a narrower
// one on assignment.
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
