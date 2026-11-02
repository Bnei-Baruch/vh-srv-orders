package importers

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	pkgmocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// withFailingDB makes Init fail at the repo step: CreateEmitter still succeeds
// with an empty NatsUrl, and nothing listens on port 1.
func withFailingDB(t *testing.T) {
	t.Helper()

	// The value, not the pointer: common.Config is *envConfig, so saving the
	// pointer and handing it back restores nothing.
	saved := *common.Config
	t.Cleanup(func() { *common.Config = saved })

	common.Config.NatsUrl = ""
	common.Config.PgHost, common.Config.PgPort = "127.0.0.1", "1"
}

// Close runs on the Init failure path, so it has to survive a partial Init.
// Driving Init for real is the point: calling Close on a zero-value struct, as
// this test first did, passes whether or not the bug is present.
func TestBaseImporterCloseAfterFailedInit(t *testing.T) {
	withFailingDB(t)

	im := NewBaseImporter()
	require.Error(t, im.Init(), "the repo step must fail for this test to mean anything")
	// == nil, not require.Nil, which reflects into the interface and reports a
	// boxed nil pointer as nil — the one thing this line exists to catch.
	require.True(t, im.repo == nil, "a failed Init must leave no typed nil behind")

	im.Close()
}

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
