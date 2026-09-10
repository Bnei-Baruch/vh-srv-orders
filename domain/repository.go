package domain

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// AccountsRepo is the storage this package uses. Declared here rather than in
// repo so that the rest of OrdersRepository — and its 6900-line mock — is not
// something domain depends on. *repo.OrdersDB satisfies it implicitly.
//
// No Close(): the pool's lifetime belongs to App.Shutdown and the cmd
// entrypoints.
type AccountsRepo interface {
	GetOrCreateAccountFromProfile(ctx context.Context, keycloakID string) (int, error)
	GetAccountIDByKeycloakID(ctx context.Context, keycloakId string) (int, error)
	PatchOrCreateAccount(ctx context.Context, a repo.Account) (int, error)
	SoftDeleteAccount(ctx context.Context, accountID int) error
}
