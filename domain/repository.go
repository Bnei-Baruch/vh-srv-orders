package domain

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// AccountsRepo is the storage this package actually uses: the four account
// calls the profile event handler makes. It is declared here, in the consumer,
// rather than next to its implementation.
//
// The difference is what a change costs. `repo.OrdersRepository` is one
// interface of a hundred-odd methods that every consumer takes whole, so
// adding a coupon method changes the type `domain` depends on, and the mock
// `domain`'s tests build. Declared here, the dependency is four lines that
// only move when this package's own needs move.
//
// *repo.OrdersDB satisfies this without being told to — no import from repo to
// domain, nothing in repo to keep in sync. Note what is absent: Close(). The
// pool's lifetime belongs to App.Shutdown and the cmd entrypoints, and an
// interface that does not mention it cannot end up closing the pool from
// inside an event handler.
type AccountsRepo interface {
	GetOrCreateAccountFromProfile(ctx context.Context, keycloakID string) (int, error)
	GetAccountIDByKeycloakID(ctx context.Context, keycloakId string) (int, error)
	PatchOrCreateAccount(ctx context.Context, a repo.Account) (int, error)
	SoftDeleteAccount(ctx context.Context, accountID int) error
}
