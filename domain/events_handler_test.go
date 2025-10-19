package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/mock"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/internal/mocks"
	mockspkg "gitlab.bbdev.team/vh/pay/orders/internal/mocks/pkg"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles/profilestest"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func TestCreateProfile(t *testing.T) {
	p := profilestest.ProfileFixture()

	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.EXPECT().GetOrCreateAccountFromProfile(
		mock.MatchedBy(isExpectedContext), p.KeycloakID.String()).
		Return(1, nil)

	eh := NewEventsHandler(ordersRepo)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeCreateProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
				"email":       p.PrimaryEmail,
			}),
	)
}

func TestCreateProfileMissingKeycloakID(t *testing.T) {
	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.AssertNotCalled(t, "GetOrCreateAccountFromProfile", mock.Anything, mock.Anything)

	eh := NewEventsHandler(ordersRepo)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeCreateProfile,
			map[string]interface{}{
				"email": "user@example.com",
			}),
	)
}

func TestUpdateProfile(t *testing.T) {
	p := profilestest.ProfileFixture()

	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.EXPECT().PatchOrCreateAccount(
		mock.MatchedBy(isExpectedContext),
		mock.MatchedBy(makeProfileAccountMatcher(p)),
	).Return(1, nil)

	eh := NewEventsHandler(ordersRepo)

	profileService := mockspkg.NewMockProfileService(t)
	eh.SetProfileService(profileService)
	profileService.EXPECT().GetProfileByKeycloakID(
		mock.MatchedBy(isExpectedContext),
		p.KeycloakID.String()).
		Return(p, nil)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeUpdateProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
				"email":       "user@example.com",
			}),
	)
}

func TestUpdateProfileMissingKeycloakID(t *testing.T) {
	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.AssertNotCalled(t, "PatchOrCreateAccount", mock.Anything, mock.Anything)

	eh := NewEventsHandler(ordersRepo)

	profileService := mockspkg.NewMockProfileService(t)
	eh.SetProfileService(profileService)
	profileService.AssertNotCalled(t, "GetProfileByKeycloakID", mock.Anything, mock.Anything)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeUpdateProfile,
			map[string]interface{}{
				"email": "user@example.com",
			}),
	)
}

func TestUpdateProfileNotFound(t *testing.T) {
	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.AssertNotCalled(t, "PatchOrCreateAccount", mock.Anything, mock.Anything)

	eh := NewEventsHandler(ordersRepo)

	profileService := mockspkg.NewMockProfileService(t)
	eh.SetProfileService(profileService)

	p := profilestest.ProfileFixture()
	profileService.EXPECT().GetProfileByKeycloakID(
		mock.MatchedBy(isExpectedContext),
		p.KeycloakID.String()).
		Return(nil, profiles.ErrNotFound)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeUpdateProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
				"email":       p.PrimaryEmail,
			}),
	)
}

func TestUpdateProfileServiceError(t *testing.T) {
	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.AssertNotCalled(t, "PatchOrCreateAccount", mock.Anything, mock.Anything)

	eh := NewEventsHandler(ordersRepo)

	profileService := mockspkg.NewMockProfileService(t)
	eh.SetProfileService(profileService)

	p := profilestest.ProfileFixture()
	profileService.EXPECT().GetProfileByKeycloakID(
		mock.MatchedBy(isExpectedContext),
		p.KeycloakID.String()).
		Return(nil, errors.New("some communication error"))

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeUpdateProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
				"email":       p.PrimaryEmail,
			}),
	)
}

func TestDeleteProfile(t *testing.T) {
	p := profilestest.ProfileFixture()

	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.EXPECT().GetAccountIDByKeycloakID(
		mock.MatchedBy(isExpectedContext),
		p.KeycloakID.String(),
	).Return(1, nil)
	ordersRepo.EXPECT().SoftDeleteAccount(
		mock.MatchedBy(isExpectedContext),
		1,
	).Return(nil)

	eh := NewEventsHandler(ordersRepo)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeDeleteProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
			}),
	)
}

func TestDeleteProfileNotFound(t *testing.T) {
	p := profilestest.ProfileFixture()

	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.EXPECT().GetAccountIDByKeycloakID(
		mock.MatchedBy(isExpectedContext),
		p.KeycloakID.String(),
	).Return(0, pgx.ErrNoRows)
	ordersRepo.AssertNotCalled(t, "SoftDeleteAccount", mock.Anything, mock.Anything)

	eh := NewEventsHandler(ordersRepo)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeDeleteProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
			}),
	)
}

func TestDeleteProfileDBErr(t *testing.T) {
	p := profilestest.ProfileFixture()

	ordersRepo := mocks.NewMockOrdersRepository(t)
	ordersRepo.EXPECT().GetAccountIDByKeycloakID(
		mock.MatchedBy(isExpectedContext),
		p.KeycloakID.String(),
	).Return(0, errors.New("some db error"))
	ordersRepo.AssertNotCalled(t, "SoftDeleteAccount", mock.Anything, mock.Anything)

	eh := NewEventsHandler(ordersRepo)

	eh.HandleProfilesEvent(
		profilestest.EventFixture(
			profiles.TypeDeleteProfile,
			map[string]interface{}{
				"keycloak_id": p.KeycloakID.String(),
			}),
	)
}

func isExpectedContext(ctx context.Context) bool {
	builder, ok := ctx.Value(common.CtxEventBuilder).(events.EventBuilder)
	if !ok {
		return false
	}
	e := builder.BuildEvent("test", nil)
	if e.Component != events.ComponentProfileEventHandler || e.Actor != events.ActorSystem {
		return false
	}

	_, ok = ctx.Value(common.CtxTokenSource).(keycloak.TokenSource)
	if !ok {
		return false
	}

	return true
}

func makeProfileAccountMatcher(p *profiles.Profile) func(repo.Account) bool {
	return func(a repo.Account) bool {
		return isExpectedAccount(p, a)
	}
}

func isExpectedAccount(p *profiles.Profile, a repo.Account) bool {
	if a.UserKey.String != p.KeycloakID.String() {
		return false
	}
	if a.Email.String != *p.PrimaryEmail {
		return false
	}
	if a.FirstName.String != *p.FirstNameVernacular {
		return false
	}
	if a.LastName.String != *p.LastNameVernacular {
		return false
	}
	if a.Country.String != *p.Country {
		return false
	}
	if a.City.String != *p.City {
		return false
	}
	if a.State.String != *p.StateOrRegion {
		return false
	}
	if a.Street.String != *p.StreetAddress {
		return false
	}
	if a.Postcode.String != *p.PostalCode {
		return false
	}
	if a.Phone.String != *p.MobileNumber {
		return false
	}
	if a.DeletedAt != nil && !p.Deleted ||
		a.DeletedAt == nil && p.Deleted {
		return false
	}
	return true
}
