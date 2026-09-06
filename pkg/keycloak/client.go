package keycloak

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

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

	// mu guards token and claims, which are read and replaced together.
	//
	// It is held across the login and refresh calls, deliberately. One holder
	// logging in while the others wait is the point: the alternative is every
	// caller that saw the same expired token starting its own login. The
	// renewal run charges through maxWorkers goroutines sharing one of these,
	// so an expiry crossing would otherwise cost one login per worker.
	mu     sync.Mutex
	token  *gocloak.JWT
	claims *jwt.MapClaims
	// lastFailure is when the most recent attempt to obtain a token failed. It
	// short-circuits the next callers for loginFailureBackoff, so a Keycloak that
	// is down or hanging fails the run quickly instead of every call paying a
	// timeout in turn with the mutex held.
	lastFailure time.Time
}

func NewClient(scopes ...string) *Client {
	c := new(Client)
	c.kc = gocloak.NewClient(common.Config.KeycloakServerUrl)
	gocloak.SetLegacyWildFlySupport()(c.kc)

	// gocloak leaves its resty client without a deadline, so a Keycloak that
	// accepts the connection and never answers would hang the caller forever.
	// That is worse now than it was: the mutex below serialises waiters, so an
	// unbounded stall would be paid once per waiting charge worker rather than
	// once in parallel. Bounded here instead of threading a context through
	// Token(), which is keycloak.TokenSource's signature and would mean
	// regenerating every mock of it.
	c.kc.RestyClient().SetTimeout(tokenRequestTimeout)

	c.scopes = scopes
	return c
}

// tokenRequestTimeout bounds one HTTP request, which is not the same as one
// AccessToken call: a single holder of the mutex can issue up to four — refresh,
// its certificate fetch, then login and its own certificate fetch — so the worst
// case per holder is four times this, not one.
const tokenRequestTimeout = 10 * time.Second

// loginFailureBackoff is how long a failed attempt suppresses the next one.
//
// Without it a Keycloak that accepts connections and never answers costs every
// caller the full timeout, with the mutex held, because nothing is cached on
// failure: a renewal run of a few thousand orders across two terminal legs would
// grind serially for hours rather than failing. Token() passes
// context.Background(), so cancelling the run cannot interrupt those waits
// either — the worker loop only checks ctx between orders.
//
// Short, because it makes callers fail while it lasts: long enough to collapse a
// stampede against a dead Keycloak, short enough that a blip costs one window.
const loginFailureBackoff = 5 * time.Second

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

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.backingOff() {
		utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() skipped, a recent attempt failed",
			slog.Duration("backoff", loginFailureBackoff))
		return ""
	}

	// we have no token, let's login
	if c.token == nil {
		if err = c.login(ctx); err != nil {
			utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() error login", slog.Any("err", err))
			c.lastFailure = time.Now()
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
		c.lastFailure = time.Now()
		return ""
	}
	if err = c.claims.Valid(); err != nil {
		utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() login after failed refresh got invalid claims", slog.Any("err", err))
		c.lastFailure = time.Now()
		return ""
	}

	return c.token.AccessToken
}

// backingOff reports whether a recent failure should suppress this attempt.
// Called with mu held.
func (c *Client) backingOff() bool {
	return !c.lastFailure.IsZero() && time.Since(c.lastFailure) < loginFailureBackoff
}

// Invalidate clears the cached token, forcing a fresh login on next Token() call
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.token = nil
	c.claims = nil
}

// InvalidateToken clears the cache only if stale is still what is cached, so a
// caller reacting to a 401 cannot discard a token some other caller has already
// replaced.
//
// Without the comparison, N workers holding the same expired token each clear
// the cache in turn: the first replaces it, the second throws that replacement
// away, and one expiry costs a login per worker. Worse, if the credential is
// rejected for a reason a new token cannot fix — a missing scope for the route,
// say — every charge in the run triggers its own login, which is a burst large
// enough to look like an attack on the service account.
func (c *Client) InvalidateToken(stale string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token == nil || c.token.AccessToken != stale {
		return
	}

	c.token = nil
	c.claims = nil
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
	c.lastFailure = time.Time{}

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
	c.lastFailure = time.Time{}

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
