package pelecard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

type PelecardAPI interface {
	FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error)
	ChargeByToken(ctx context.Context, request *ChargeRequest, terminal Terminal) (map[string]interface{}, error)
}

// Client is a client for interacting with Pelecard API
type Client struct {
	Client *resty.Client
	// BaseURL is external_payments. Overridable for the same reason
	// Terminal.ChargeURL is: so tests can point at a stub.
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
