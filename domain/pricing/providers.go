package pricing

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// The discount lookups pricing needs from storage. They were already one
// method each; they lived in repo, with doc comments naming this package as
// their consumer. *repo.OrdersDB satisfies all three implicitly.
//
// Unexported: nothing outside this package names them, and they have no
// generated mocks — pricing's tests use function adapters. Exporting a
// consumer-owned interface re-invites the cross-package sharing that having it
// here is meant to end.
type (
	// manualDiscountProvider fetches the active manual discount for a user.
	manualDiscountProvider interface {
		GetActiveManualDiscount(ctx context.Context, keycloakID string) (*repo.ManualDiscount, error)
	}

	// hhGrantProvider fetches the active Help Haver grant for a user.
	hhGrantProvider interface {
		GetActiveHHGrant(ctx context.Context, keycloakID string) (*repo.HHGrant, error)
	}

	// couponProvider fetches a member's active coupon redemptions for pricing.
	couponProvider interface {
		GetActiveCouponRedemptions(ctx context.Context, keycloakID string) ([]repo.ActiveCouponRedemption, error)
	}
)
