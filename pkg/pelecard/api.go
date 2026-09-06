package pelecard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// ErrUnauthorized means external_payments rejected this service's credential —
// after one retry with a fresh token, so it is not a stale-token race. Every
// charge in a run will fail the same way, which is why it is a distinct error
// rather than one more gateway status.
var ErrUnauthorized = errors.New("external_payments rejected the credential")

type PelecardAPI interface {
	FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error)
	ChargeByToken(ctx context.Context, request *ChargeRequest, terminal Terminal) (map[string]interface{}, error)
}

// Client calls external_payments, which holds the Pelecard credentials and
// terminals on this service's behalf.
type Client struct {
	Client *resty.Client
	// BaseURL is overridable so tests can point at a stub, not configurable:
	// the host is hardcoded in five other places here too. See issue #22.
	BaseURL string

	// Tokens authenticates calls to external_payments.
	Tokens keycloak.TokenSource
}

// NewClient creates a client for external_payments. No Pelecard credentials and
// no terminal number: this service no longer talks to Pelecard.
func NewClient() *Client {
	client := resty.New()
	client.SetHeaders(map[string]string{
		"Content-Type": "application/json",
	})

	return &Client{
		Client:  client,
		BaseURL: EXTERNAL_PAYMENTS_BASE_URL,
		Tokens:  keycloak.NewClient(),
	}
}

// sendAuthorized runs one request to external_payments with a Keycloak bearer,
// retrying once on 401 with a fresh token.
//
// The header goes on the request, never on the shared resty client, which every
// call this type makes reuses. TestFetchMuhlafim_TokenNotSetOnSharedClient pins
// that.
//
// One implementation for both calls: an access token lives 15 minutes and the
// renewal run is a burst of thousands of charges, so a run crosses an expiry,
// and MapClaims.Valid() applies no clock leeway. The verb is the caller's —
// muhlafim reads over GET, a charge posts.
func (c *Client) sendAuthorized(ctx context.Context, what string,
	do func(*resty.Request) (*resty.Response, error)) (*resty.Response, error) {

	send := func() (*resty.Response, string, error) {
		token, err := c.Tokens.Token()
		if err != nil {
			return nil, "", fmt.Errorf("keycloak token for external_payments: %w", err)
		}

		resp, err := do(c.Client.NewRequest().
			SetContext(ctx).
			SetHeader("Authorization", "Bearer "+token))
		if err != nil {
			return nil, token, fmt.Errorf("external %s request failed: %w", what, err)
		}

		return resp, token, nil
	}

	resp, token, err := send()
	if err != nil || resp.StatusCode() != http.StatusUnauthorized {
		return resp, err
	}

	// Clear only the token that was actually rejected. The renewal run shares one
	// keycloak.Client across its workers, so several of them can be holding the
	// same expired token and get 401 together; an unconditional Invalidate would
	// have each in turn discard the replacement the previous one just fetched,
	// turning one expiry into a login per worker.
	c.invalidate(token)

	utils.LogFor(ctx).Warn("external_payments returned 401, retrying with a fresh token",
		slog.String("call", what))

	resp, _, err = send()

	return resp, err
}

// invalidate drops the rejected token, comparing before clearing where the
// source supports it.
//
// keycloak.TokenSource only promises Invalidate(), and widening that interface
// would mean regenerating the mocks and touching pkg/accounting and
// pkg/profiles, which have no concurrent callers and no reason to change here.
// keycloak.Client — the only source used in production on this path — carries
// the comparing form.
func (c *Client) invalidate(stale string) {
	if source, ok := c.Tokens.(interface{ InvalidateToken(string) }); ok {
		source.InvalidateToken(stale)
		return
	}

	c.Tokens.Invalidate()
}

// FetchMuhlafim returns Pelecard's card replacements for a date window from
// external_payments, keyed by the token being replaced. An empty window is a
// normal answer, not an error.
func (c *Client) FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error) {
	if c.Tokens == nil {
		return nil, fmt.Errorf("no token source configured for external_payments")
	}

	resp, err := c.sendAuthorized(ctx, "muhlafim", func(r *resty.Request) (*resty.Response, error) {
		return r.SetQueryParams(map[string]string{"StartDate": startDate, "EndDate": endDate}).
			Get(c.BaseURL + "/token/muhlafim")
	})
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, fmt.Errorf("external muhlafim error [%d]: %s", resp.StatusCode(), resp.String())
	}

	var entries map[string]MuhlafimEntry
	if err := json.Unmarshal(resp.Body(), &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal external muhlafim response: %w", err)
	}

	return entries, nil
}

// ChargeByToken sends a token-based charge request to the payment gateway.
//
// It authenticates: until now these calls arrived at external_payments with no
// credential at all, which is why the monthly renewal burst shows up in its log
// as `requested_by=anonymous` and why the organization still has to be sent in
// the body. See sendAuthorized for where the header goes and why.
func (c *Client) ChargeByToken(ctx context.Context, request *ChargeRequest, terminal Terminal) (map[string]interface{}, error) {
	log := utils.LogFor(ctx)

	if terminal.ChargeURL == "" {
		return nil, fmt.Errorf("no charge URL for terminal %q", terminal.Name)
	}
	if c.Tokens == nil {
		return nil, fmt.Errorf("no token source configured for external_payments")
	}

	resp, err := c.sendAuthorized(ctx, "charge", func(r *resty.Request) (*resty.Response, error) {
		return r.SetBody(request).Post(terminal.ChargeURL)
	})
	if err != nil {
		return nil, err
	}

	log.Info("charge gateway response",
		slog.String("terminal", terminal.Name),
		slog.Int("http_status", resp.StatusCode()),
		slog.Int("body_size", len(resp.Body())))

	if resp.IsError() {
		log.Error("charge gateway HTTP error",
			slog.String("terminal", terminal.Name),
			slog.Int("http_status", resp.StatusCode()),
			slog.String("body", string(resp.Body())))

		// A rejected credential is worth telling apart from a declined card: one
		// is a deployment fault that fails every charge in the run, the other is
		// this member's card. The charge-check command turns on that distinction,
		// and matching an error string for it would be fragile.
		if resp.StatusCode() == http.StatusUnauthorized {
			return nil, fmt.Errorf("%w: charge gateway HTTP error [%d]",
				ErrUnauthorized, resp.StatusCode())
		}

		return nil, fmt.Errorf("charge gateway HTTP error [%d]", resp.StatusCode())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		log.Error("charge gateway unmarshal error",
			slog.String("terminal", terminal.Name),
			slog.String("body", string(resp.Body())),
			slog.Any("err", err))
		return nil, fmt.Errorf("failed to unmarshal charge response: %w", err)
	}

	return result, nil
}

// Execute implements ChargeExecutor by delegating to ChargeByToken.
// The orderID parameter is ignored — it exists for dry-run determinism only.
func (c *Client) Execute(ctx context.Context, request *ChargeRequest, terminal Terminal, _ uint) (map[string]interface{}, error) {
	return c.ChargeByToken(ctx, request, terminal)
}
