package repo

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v4"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	mocks "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles/profilestest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

func TestGetAccountIDByKeycloakID(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.Nil(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.Nil(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	id, err := db.CreateAccount(ctx, Account{
		UserKey: null.StringFrom("keycloak123"),
		Email:   null.StringFrom("user@example.com"),
	})
	require.Nil(t, err)
	require.Greater(t, id, 0)

	gotID, err := db.GetAccountIDByKeycloakID(ctx, "keycloak123")
	require.Nil(t, err)
	require.Equal(t, id, gotID)

	gotID, err = db.GetAccountIDByKeycloakID(ctx, "nonexistent")
	require.NotNil(t, err)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, 0, gotID)
}

func TestGetOrCreateAccountFromProfile(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.Nil(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.Nil(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	profileService := mocks.NewMockProfileService(t)
	db.SetProfileService(profileService)
	profile := profilestest.ProfileFixture()
	profileService.EXPECT().LookupProfileByKeycloakId(mock.Anything, profile.KeycloakID.String()).
		Return(profile, nil).
		Once()

	id, err := db.GetOrCreateAccountFromProfile(ctx, profile.KeycloakID.String())
	require.Nil(t, err)
	require.Greater(t, id, 0)
	account, err := db.GetAccount(ctx, id, "")
	require.Nil(t, err)
	require.Equal(t, profile.KeycloakID.String(), account.UserKey.String)
	require.Equal(t, *profile.PrimaryEmail, account.Email.String)
	require.Equal(t, *profile.FirstNameVernacular, account.FirstName.String)
	require.Equal(t, *profile.LastNameVernacular, account.LastName.String)
	require.Equal(t, *profile.Country, account.Country.String)
	require.Equal(t, *profile.City, account.City.String)
	require.Equal(t, *profile.StateOrRegion, account.State.String)
	require.Equal(t, *profile.StreetAddress, account.Street.String)
	require.Equal(t, *profile.PostalCode, account.Postcode.String)
	require.Equal(t, *profile.MobileNumber, account.Phone.String)
	if profile.Deleted {
		require.False(t, account.DeletedAt.IsZero())
	} else {
		require.Nil(t, account.DeletedAt)
	}

	// Second call should return the same account without calling ProfileService.
	id2, err := db.GetOrCreateAccountFromProfile(ctx, profile.KeycloakID.String())
	require.Nil(t, err)
	require.Equal(t, id, id2)
	require.Equal(t, profile.KeycloakID.String(), account.UserKey.String)
	require.Equal(t, *profile.PrimaryEmail, account.Email.String)
	require.Equal(t, *profile.FirstNameVernacular, account.FirstName.String)
	require.Equal(t, *profile.LastNameVernacular, account.LastName.String)
	require.Equal(t, *profile.Country, account.Country.String)
	require.Equal(t, *profile.City, account.City.String)
	require.Equal(t, *profile.StateOrRegion, account.State.String)
	require.Equal(t, *profile.StreetAddress, account.Street.String)
	require.Equal(t, *profile.PostalCode, account.Postcode.String)
	require.Equal(t, *profile.MobileNumber, account.Phone.String)
	if profile.Deleted {
		require.False(t, account.DeletedAt.IsZero())
	} else {
		require.Nil(t, account.DeletedAt)
	}
}

func TestPatchOrCreateAccount(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.Nil(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.Nil(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	account := AccountFixture()
	id, err := db.PatchOrCreateAccount(ctx, account)
	require.Nil(t, err)
	require.Greater(t, id, 0)

	gotAccount, err := db.GetAccount(ctx, id, "")
	require.Nil(t, err)
	require.Equal(t, account.UserKey.String, gotAccount.UserKey.String)
	require.Equal(t, account.Email.String, gotAccount.Email.String)
	require.Equal(t, account.FirstName.String, gotAccount.FirstName.String)
	require.Equal(t, account.LastName.String, gotAccount.LastName.String)
	require.Equal(t, account.Phone.String, gotAccount.Phone.String)
	require.Equal(t, account.Street.String, gotAccount.Street.String)
	require.Equal(t, account.City.String, gotAccount.City.String)
	require.Equal(t, account.State.String, gotAccount.State.String)
	require.Equal(t, account.Postcode.String, gotAccount.Postcode.String)
	require.Equal(t, account.Country.String, gotAccount.Country.String)
	require.Equal(t, account.AccountType.String, gotAccount.AccountType.String)

	// Update some fields
	account.Email = null.StringFrom("test2@example.com")
	account.FirstName = null.StringFrom("First2")
	account.LastName = null.StringFrom("Last2")
	account.Phone = null.StringFrom("Phone2")
	account.Street = null.StringFrom("Street2")
	account.City = null.StringFrom("City2")
	account.State = null.StringFrom("State2")
	account.Postcode = null.StringFrom("Postcode2")
	account.Country = null.StringFrom("Country2")
	account.AccountType = null.StringFrom("AccountType2")

	id2, err := db.PatchOrCreateAccount(ctx, account)
	require.Nil(t, err)
	require.Equal(t, id, id2)

	gotAccount, err = db.GetAccount(ctx, id, "")
	db.PatchOrCreateAccount(ctx, Account{})
	require.Nil(t, err)
	require.Equal(t, account.UserKey.String, gotAccount.UserKey.String)
	require.Equal(t, account.Email.String, gotAccount.Email.String)
	require.Equal(t, account.FirstName.String, gotAccount.FirstName.String)
	require.Equal(t, account.LastName.String, gotAccount.LastName.String)
	require.Equal(t, account.Phone.String, gotAccount.Phone.String)
	require.Equal(t, account.Street.String, gotAccount.Street.String)
	require.Equal(t, account.City.String, gotAccount.City.String)
	require.Equal(t, account.State.String, gotAccount.State.String)
	require.Equal(t, account.Postcode.String, gotAccount.Postcode.String)
	require.Equal(t, account.Country.String, gotAccount.Country.String)
	require.Equal(t, account.AccountType.String, gotAccount.AccountType.String)
}

func TestSoftDeleteAccount(t *testing.T) {
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.Nil(t, err)
	db, err := NewOrdersDBUrl(context.Background(), dbURL, new(events.NoopEmitter))
	require.Nil(t, err)
	defer db.Close()

	ctx := eventstest.WithTestEventBuilder(t, context.Background())

	account := AccountFixture()
	id, err := db.PatchOrCreateAccount(ctx, account)
	require.Nil(t, err)
	require.Greater(t, id, 0)

	err = db.SoftDeleteAccount(ctx, id)
	require.Nil(t, err)

	gotAccount, err := db.GetAccount(ctx, id, "")
	require.Nil(t, err)
	require.NotNil(t, gotAccount.DeletedAt)
	require.False(t, gotAccount.DeletedAt.IsZero())
}

func AccountFixture() Account {
	return Account{
		UserKey:     null.StringFrom(uuid.NewV4().String()),
		Email:       null.StringFrom("email"),
		FirstName:   null.StringFrom("first name"),
		LastName:    null.StringFrom("last name"),
		Phone:       null.StringFrom("phone"),
		Street:      null.StringFrom("street"),
		City:        null.StringFrom("city"),
		State:       null.StringFrom("state"),
		Postcode:    null.StringFrom("postcode"),
		Country:     null.StringFrom("country"),
		AccountType: null.StringFrom("account type"),
	}
}
