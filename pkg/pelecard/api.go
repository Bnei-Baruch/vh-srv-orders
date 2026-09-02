package pelecard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

type PelecardAPI interface {
	FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error)
	ChargeByToken(ctx context.Context, request *ChargeRequest, terminal Terminal) (map[string]interface{}, error)
}

// Client calls external_payments, which holds the Pelecard credentials and
// terminals on this service's behalf.
type Client struct {
	Client *resty.Client
	// BaseURL is external_payments, overridable so tests can point at a stub.
	// Not configurable: checkout's host is hardcoded in five other places here
	// too (api/transaction_handler.go, common/consts.go), and a seam that covers
	// one of six would imply staging is safe when it is not. See issue #22.
	BaseURL string

	// Tokens authenticates calls to external_payments.
	Tokens keycloak.TokenSource
}

// NewClient creates a client for external_payments. It holds no Pelecard
// credentials and no terminal number: this service no longer talks to Pelecard.
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

// postMuhlafim sends one /token/muhlafim request with a freshly taken token.
func (c *Client) postMuhlafim(ctx context.Context, startDate, endDate string) (*resty.Response, error) {
	token, err := c.Tokens.Token()
	if err != nil {
		return nil, fmt.Errorf("keycloak token for external_payments: %w", err)
	}

	resp, err := c.Client.NewRequest().
		SetContext(ctx).
		SetBody(&ExternalMuhlafimRequest{StartDate: startDate, EndDate: endDate}).
		SetHeader("Authorization", "Bearer "+token).
		Post(c.BaseURL + "/token/muhlafim")
	if err != nil {
		return nil, fmt.Errorf("external muhlafim request failed: %w", err)
	}

	return resp, nil
}

// FetchMuhlafim returns Pelecard's card replacements for a date window, keyed by
// the token being replaced, from external_payments.
//
// This service used to query Pelecard directly for it — the only reason it held
// Pelecard credentials and a terminal number at all. Both sources were kept
// callable until a comparison over six windows agreed entry by entry and field
// by field, and the direct call was then removed along with the credentials.
func (c *Client) FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error) {
	if c.Tokens == nil {
		return nil, fmt.Errorf("no token source configured for external_payments")
	}

	resp, err := c.postMuhlafim(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}

	// Retried once on 401 after invalidating the token, as pkg/accounting and
	// pkg/profiles do: MapClaims.Valid() applies no clock leeway, so a token
	// cached a moment before expiry is sent, rejected, and would otherwise abort
	// the monthly billing run at its muhlafim step.
	if resp.StatusCode() == http.StatusUnauthorized {
		c.Tokens.Invalidate()
		utils.LogFor(ctx).Warn("external muhlafim returned 401, retrying with a fresh token")

		if resp, err = c.postMuhlafim(ctx, startDate, endDate); err != nil {
			return nil, err
		}
	}

	if resp.IsError() {
		return nil, fmt.Errorf("external muhlafim error [%d]: %s", resp.StatusCode(), resp.String())
	}

	var entries map[string]MuhlafimEntry
	if err := json.Unmarshal(resp.Body(), &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal external muhlafim response: %w", err)
	}

	// The direct Pelecard call this replaces built the map itself, so the key was
	// the entry's own token by construction. Reading a map off the wire loses
	// that, and both ways of losing it fail silently: an entry keyed by "" — or
	// keyed by anything that is not its own token — matches nothing in a caller's
	// token map, and an HTTP-200 envelope whose values happen to be objects, say
	// {"error":{...}}, unmarshals into one junk entry and reports as a window with
	// no replacements. external_payments does key by entry.Token
	// (pelecard/pelecard.go:250) and drops empty ones, so this guards the contract
	// rather than filtering anything in practice.
	for key, entry := range entries {
		if key != "" && key == entry.Token {
			continue
		}
		delete(entries, key)
		utils.LogFor(ctx).Warn("dropped muhlafim entry whose key is not its token",
			slog.Bool("key_empty", key == ""),
			slog.Bool("token_empty", entry.Token == ""),
			slog.String("source", c.BaseURL))
	}

	return entries, nil
}

// ChargeByToken sends a token-based charge request to the payment gateway.
func (c *Client) ChargeByToken(ctx context.Context, request *ChargeRequest, terminal Terminal) (map[string]interface{}, error) {
	log := utils.LogFor(ctx)

	if terminal.ChargeURL == "" {
		return nil, fmt.Errorf("no charge URL for terminal %q", terminal.Name)
	}

	resp, err := c.Client.NewRequest().
		SetContext(ctx).
		SetBody(request).
		Post(terminal.ChargeURL)
	if err != nil {
		return nil, fmt.Errorf("charge request failed: %w", err)
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
