package keycloak

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Nerzal/gocloak/v13"
	"github.com/golang-jwt/jwt/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// Client is a base for authenticated service to service communication via Keycloak.
// After instantiation one should simply call AccessToken() to obtain a valid access token.
// Note: to interact with the Keycloak API itself see vh-srv-profile for a similar class
type Client struct {
	kc     *gocloak.GoCloak
	scopes []string
	token  *gocloak.JWT
	claims *jwt.MapClaims
}

func NewClient(scopes ...string) *Client {
	c := new(Client)
	c.kc = gocloak.NewClient(common.Config.KeycloakServerUrl)
	gocloak.SetLegacyWildFlySupport()(c.kc)
	c.scopes = scopes
	return c
}

func (c *Client) Token() (string, error) {
	token := c.AccessToken(context.Background())
	if token == "" {
		return "", errors.New("unable to obtain access token")
	}
	return token, nil
}

// AccessToken will return a valid access token or an empty string.
// We'll login on the first call. Subsequent calls will reuse the obtained token.
// If a token expires we'll try to refresh it a long as we can. If not we'll try to login again.
func (c *Client) AccessToken(ctx context.Context) string {
	var err error

	// we have no token, let's login
	if c.token == nil {
		if err = c.login(ctx); err != nil {
			utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() error login", slog.Any("err", err))
			return ""
		}
	}

	// we have a token now, let's use it if it's valid
	if err = c.claims.Valid(); err == nil {
		return c.token.AccessToken
	}

	// our token has probably expired, let's try to refresh
	if err = c.refresh(ctx, c.token.RefreshToken); err == nil {
		if err = c.claims.Valid(); err == nil {
			return c.token.AccessToken
		}
	}
	utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() error refreshing token", slog.Any("err", err))

	// we are not able to refresh, we'll have to try and login again
	if err = c.login(ctx); err != nil {
		utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() error login after failed refresh", slog.Any("err", err))
		return ""
	}
	if err = c.claims.Valid(); err != nil {
		utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() login after failed refresh got invalid claims", slog.Any("err", err))
		return ""
	}

	return c.token.AccessToken
}

func (c *Client) login(ctx context.Context) error {
	token, err := c.kc.LoginClient(ctx,
		common.Config.KeycloakClientID,
		common.Config.KeycloakClientSecret,
		common.Config.KeycloakRealm,
		c.scopes...)
	if err != nil {
		return fmt.Errorf("kc.LoginClient: %w", err)
	}

	if err = c.decodeToken(ctx, token.AccessToken); err != nil {
		return fmt.Errorf("c.decodeToken (login): %w", err)
	}

	c.token = token

	return nil
}

func (c *Client) refresh(ctx context.Context, refreshToken string) error {
	token, err := c.kc.RefreshToken(ctx, refreshToken,
		common.Config.KeycloakClientID,
		common.Config.KeycloakClientSecret,
		common.Config.KeycloakRealm)
	if err != nil {
		return fmt.Errorf("kc.RefreshToken: %w", err)
	}

	if err = c.decodeToken(ctx, token.AccessToken); err != nil {
		return fmt.Errorf("c.decodeToken (refresh): %w", err)
	}

	c.token = token

	return nil
}

func (c *Client) decodeToken(ctx context.Context, token string) error {
	_, claims, err := c.kc.DecodeAccessToken(ctx, token, common.Config.KeycloakRealm)
	if err != nil {
		return fmt.Errorf("kc.DecodeAccessToken: %w", err)
	}
	c.claims = claims
	return nil
}
