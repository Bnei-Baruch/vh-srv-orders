package importers

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pkgmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// The account is looked up by email, but an email can be stale while the
// profile's keycloak id already has an account. A plain insert would then hit
// accounts_userkey_uniq and the order would be skipped; the importer must
// resolve by key instead.
func TestGetOrCreateAccount_ResolvesByKeyWhenEmailMisses(t *testing.T) {
	ctx := context.Background()
	ordersRepo := mocks.NewMockOrdersRepository(t)
	profileService := pkgmocks.NewMockProfileService(t)
	im := &BaseImporter{repo: ordersRepo, profileService: profileService}

	kc := uuid.NewV4()
	email := "new@test.test"
	ordersRepo.EXPECT().GetAccount(mock.Anything, 0, email).Return(nil, pgx.ErrNoRows)
	profileService.EXPECT().LookupProfile(mock.Anything, email).
		Return(&profiles.Profile{KeycloakID: &kc, PrimaryEmail: &email}, nil)
	ordersRepo.EXPECT().GetOrCreateAccount(mock.Anything, mock.MatchedBy(func(a repo.Account) bool {
		return a.UserKey.String == kc.String()
	})).Return(42, nil)

	id, err := im.getOrCreateAccount(ctx, email)
	require.NoError(t, err)
	assert.Equal(t, 42, id)
}
