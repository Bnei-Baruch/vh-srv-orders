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

// ErrNoCredential means this service could not obtain a token at all — Keycloak
// is unreachable, refusing, or backing off after consecutive failures.
//
// Distinct for the same reason ErrUnauthorized is: it is not a fault of the
// terminal the charge was attempted on, and the other terminal shares this
// client, so falling back to it writes a second payment row and fails
// identically. Without the sentinel a Keycloak outage is indistinguishable from
// gateway trouble in the run summary.
var ErrNoCredential = errors.New("no credential available for external_payments")

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
// One implementation for both calls: an access token lives 15 minutes and a
// renewal run crosses an expiry, and MapClaims.Valid() applies no clock leeway.
// The verb is the caller's — muhlafim reads over GET, a charge posts.
//
// Retrying a charge cannot charge twice: external_payments suppresses a
// reference that already charged within the hour, on every charge route it
// serves — including the /emv/charge leg the fallback uses — and replays the
// original response, which this caller reads status out of. A 401 can come from
// a hop in front of the handler, after the card was charged.
//
// Needs external_payments >= 46d5102, where /emv/charge replays like the token
// routes instead of answering an empty payload.
func (c *Client) sendAuthorized(ctx context.Context, what string,
	do func(*resty.Request) (*resty.Response, error)) (*resty.Response, error) {

	send := func() (*resty.Response, string, error) {
		token, err := c.Tokens.Token()
		if err != nil {
			return nil, "", fmt.Errorf("%w: %w", ErrNoCredential, err)
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

// invalidateNamer is the comparing form of Invalidate: it names the token that
// was rejected so a shared source can refuse to clear a newer one.
//
// keycloak.TokenSource only promises Invalidate(), and widening it would mean
// regenerating the mocks and touching pkg/accounting and pkg/profiles, which
// have no concurrent callers and no reason to change here. So the comparing form
// is reached by assertion — and pinned below, because a structural assertion
// matched by name and signature at runtime would otherwise stop matching in
// silence if the method were renamed or the source wrapped by a shim that
// forwards only Token() and Invalidate(). The fallback is the unconditional
// clear this exists to avoid, so that silence would restore the stampede.
type invalidateNamer interface{ InvalidateToken(string) }

var _ invalidateNamer = (*keycloak.Client)(nil)

// invalidate drops the rejected token, comparing before clearing where the
// source supports it.
func (c *Client) invalidate(stale string) {
	if source, ok := c.Tokens.(invalidateNamer); ok {
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
		// this member's card. handleNonRetryableError (domain/billing/charge.go)
		// branches on it to fail the order once instead of retrying the other
		// terminal with the same credential, and matching an error string for
		// that would be fragile.
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
