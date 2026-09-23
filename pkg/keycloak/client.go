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

	// mu guards token and claims, and is held across login and refresh on
	// purpose: one holder logging in while the rest wait, rather than every
	// caller that saw the same expired token starting its own login. The
	// renewal run shares one client across maxWorkers goroutines.
	mu     sync.Mutex
	token  *gocloak.JWT
	claims *jwt.MapClaims
	// failures counts consecutive failed attempts and lastFailure is when the
	// most recent one happened. Together they short-circuit callers only once
	// Keycloak looks genuinely down — see backingOff.
	failures    int
	lastFailure time.Time
}

func NewClient(scopes ...string) *Client {
	c := new(Client)
	c.kc = gocloak.NewClient(common.Config.KeycloakServerUrl)
	gocloak.SetLegacyWildFlySupport()(c.kc)

	// gocloak leaves its resty client without a deadline, and the mutex above
	// serialises waiters, so a Keycloak that never answers costs the full stall
	// once per waiting worker.
	c.kc.RestyClient().SetTimeout(tokenRequestTimeout)

	c.scopes = scopes
	return c
}

// tokenRequestTimeout bounds one HTTP request, not one AccessToken call: a
// single holder of the mutex can issue four — refresh and its certificate
// fetch, then login and its own — so the worst case per holder is 4x this.
// That 4x is the floor for loginFailureBackoff, guarded below.
const tokenRequestTimeout = 10 * time.Second

// loginFailureBackoff is how long consecutive failures suppress further
// attempts, loginFailureThreshold how many it takes.
//
// Suppression exists because against a Keycloak that accepts connections and
// never answers, every caller pays the full timeout with the mutex held, and
// Token() passes context.Background() so cancelling the run cannot interrupt it.
//
// But suppression is dangerous in its own right: a charge worker that cannot get
// a token still writes a pending payment row per attempt, so a blip could book
// hundreds of orders failed. Hence a threshold, so failures with a success
// between them suppress nothing, and a window longer than one attempt's worst
// case (4x tokenRequestTimeout) so the fast-failing stretch dominates.
const (
	loginFailureBackoff   = 45 * time.Second
	loginFailureThreshold = 3
)

// Compile-time guard: the window must outlast one attempt's worst case. Strict
// and in nanoseconds, so a window exactly 4x the timeout fails and a sub-second
// overrun cannot hide behind truncation. uint64, not uint, because the value is
// ~5e9 and would overflow a 32-bit build.
//
// The 4 is a literal nothing derives: a fifth request inside the mutex changes
// no constant here, so raise the multiplier by hand if one is added.
const _ = uint64(loginFailureBackoff - 4*tokenRequestTimeout - 1)

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

	// A valid cached token is returned before anything else, so a caller holding
	// one is never refused by a backoff that concerns obtaining a new one.
	if c.token != nil {
		if err = c.claims.Valid(); err == nil {
			return c.token.AccessToken
		}
	}

	if c.backingOff() {
		utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() skipped, consecutive attempts failed",
			slog.Int("failures", c.failures),
			slog.Duration("backoff", loginFailureBackoff))
		return ""
	}

	// we have no token, let's login
	if c.token == nil {
		if err = c.login(ctx); err != nil {
			utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() error login", slog.Any("err", err))
			c.recordFailure()
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
		c.recordFailure()
		return ""
	}
	if err = c.claims.Valid(); err != nil {
		utils.LogFor(ctx).Warn("keycloak.Client.AccessToken() login after failed refresh got invalid claims", slog.Any("err", err))
		c.recordFailure()
		return ""
	}

	return c.token.AccessToken
}

// backingOff reports whether enough consecutive failures have happened recently
// to suppress this attempt. Called with mu held.
func (c *Client) backingOff() bool {
	return c.failures >= loginFailureThreshold && time.Since(c.lastFailure) < loginFailureBackoff
}

// recordFailure and clearFailures are the only writers of the failure state, so
// the threshold counts consecutive failures rather than lifetime ones. Called
// with mu held.
func (c *Client) recordFailure() {
	c.failures++
	c.lastFailure = time.Now()
}

func (c *Client) clearFailures() {
	c.failures = 0
	c.lastFailure = time.Time{}
}

// Invalidate clears the cached token, forcing a fresh login on next Token() call
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.token = nil
	c.claims = nil
	// A caller saying it knows this token is bad outranks a backoff, which would
	// otherwise turn the retry after a 401 into no retry.
	c.clearFailures()
}

// InvalidateToken clears the cache only if stale is still what is cached, so a
// caller reacting to a 401 cannot discard a token another caller just replaced.
// Without the comparison, N workers holding the same expired token clear it in
// turn and one expiry costs a login each — and if the credential is rejected for
// a reason no new token fixes, every charge in the run logs in.
func (c *Client) InvalidateToken(stale string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token == nil || c.token.AccessToken != stale {
		return
	}

	c.token = nil
	c.claims = nil
	// As in Invalidate: an explicit "this token is bad" outranks the backoff, so
	// the 401 retry actually gets an attempt.
	c.clearFailures()
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
	c.clearFailures()

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
	c.clearFailures()

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
