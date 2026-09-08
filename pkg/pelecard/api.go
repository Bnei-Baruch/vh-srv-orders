package pelecard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// ErrUnauthorized means external_payments rejected the credential, after one
// retry with a fresh token, so it is not a stale-token race. Every charge in the
// run fails the same way.
var ErrUnauthorized = errors.New("external_payments rejected the credential")

// ErrNoCredential means no token could be obtained at all. Distinct for the same
// reason as ErrUnauthorized — the other terminal shares this client and would
// fail identically — and so a Keycloak outage is not read as gateway trouble in
// the run summary.
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

// chargeRequestTimeout bounds a call to external_payments. Needed because the
// charge context is uncancellable by design (context.WithoutCancel in
// domain/billing/renewal.go), so the parent deadline is gone and a hung peer
// would hold a worker for the life of the process.
//
// Above external_payments' own 120s WriteTimeout, deliberately: abandoning a
// charge still in flight makes chargeResolved retry on the EMV terminal under a
// different reference, which the duplicate suppression there cannot match, and
// the member is charged twice. Past its write deadline it cannot answer anyway.
const chargeRequestTimeout = 150 * time.Second

// NewClient creates a client for external_payments. No Pelecard credentials and
// no terminal number: this service no longer talks to Pelecard.
func NewClient() *Client {
	client := resty.New()
	client.SetTimeout(chargeRequestTimeout)
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
// retrying once on 401 with a fresh token. The header goes on the request, not
// on the shared resty client every call here reuses. The verb is the caller's:
// muhlafim reads over GET, a charge posts.
//
// Retrying a charge cannot charge twice: external_payments suppresses a
// reference that already charged within the hour and replays the original
// response, on every charge route including the /emv/charge leg the fallback
// uses. Needs external_payments >= 46d5102 for that. A 401 can come from a hop
// in front of the handler, after the card was charged.
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

	// Only the token that was rejected: workers share one keycloak.Client, and an
	// unconditional Invalidate has each discard the replacement the last one
	// fetched, turning one expiry into a login per worker.
	c.invalidate(token)

	utils.LogFor(ctx).Warn("external_payments returned 401, retrying with a fresh token",
		slog.String("call", what))

	resp, _, err = send()

	return resp, err
}

// invalidateNamer is the comparing form of Invalidate: it names the rejected
// token so a shared source can refuse to clear a newer one. Reached by assertion
// because keycloak.TokenSource promises only Invalidate(), and pinned below —
// a rename would otherwise fall back to the unconditional clear in silence.
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
// It authenticates: these calls used to arrive with no credential, which is why
// the renewal burst shows up in checkout's log as `requested_by=anonymous`.
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

		// Told apart from a declined card because handleNonRetryableError
		// branches on it: one is a deployment fault that fails every charge, the
		// other is this member's card.
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
