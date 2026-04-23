package pelecard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

type PelecardAPI interface {
	FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error)
	ChargeByToken(ctx context.Context, request *ChargeRequest, terminal Terminal) (map[string]interface{}, error)
}

// Client is a client for interacting with Pelecard API
type Client struct {
	Client         *resty.Client
	User           string
	Password       string
	TerminalNumber string
}

// NewClient creates a new Pelecard client (new terminal)
func NewClient() *Client {
	return NewClientWithTerminal(common.Config.PelecardNewTerminalNumber)
}

// NewClientWithTerminal creates a new Pelecard client with a specific terminal number
func NewClientWithTerminal(terminalNumber string) *Client {
	client := resty.New()
	client.SetHeaders(map[string]string{
		"Content-Type": "application/json",
	})

	return &Client{
		Client:         client,
		User:           common.Config.PelecardUser,
		Password:       common.Config.PelecardPassword,
		TerminalNumber: terminalNumber,
	}
}

// FetchMuhlafim fetches muhlafim data from Pelecard API for the given date range
// Date format should be "DD/MM/YYYY HH:MM" (e.g., "21/08/2025 00:00")
// Returns a map of token -> MuhlafimEntry, filtering out entries with empty tokens
func (c *Client) FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error) {
	req := NewMuhlafimRequest(c.newTerminalRequest(), startDate, endDate)

	resp, err := c.Client.NewRequest().
		SetContext(ctx).
		SetBody(req).
		Post(fmt.Sprintf("%s/services/GetTerminalMuhlafim", PELECARD_API_BASE_URL))

	if err != nil {
		return nil, fmt.Errorf("pelecard muhlafim request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("pelecard API error [%d]: %s", resp.StatusCode(), resp.String())
	}

	var response MuhlafimResponse
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pelecard response: %w", err)
	}

	// Parse response into map, filtering out entries with empty tokens
	result := make(map[string]MuhlafimEntry)
	for _, entry := range response.ResultData {
		if len(entry.Token) > 0 {
			result[entry.Token] = entry
		}
	}

	return result, nil
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

func (c *Client) newTerminalRequest() TerminalRequest {
	return TerminalRequest{
		BaseRequest: BaseRequest{
			User:     c.User,
			Password: c.Password,
		},
		TerminalNumber: c.TerminalNumber,
	}
}
