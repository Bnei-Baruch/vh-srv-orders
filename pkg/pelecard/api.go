package pelecard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

type PelecardAPI interface {
	FetchMuhlafim(ctx context.Context, startDate, endDate string) (map[string]MuhlafimEntry, error)
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

func (c *Client) newTerminalRequest() TerminalRequest {
	return TerminalRequest{
		BaseRequest: BaseRequest{
			User:     c.User,
			Password: c.Password,
		},
		TerminalNumber: c.TerminalNumber,
	}
}
