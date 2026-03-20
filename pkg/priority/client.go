// Package priority provides a client for interacting with Priority ERP Cloud API.
package priority

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Client is a client for interacting with Priority ERP Cloud API
type Client struct {
	client *resty.Client
}

// NewClient creates a new Priority ERP client with basic authentication
func NewClient() *Client {
	client := resty.New()
	client.SetBaseURL(common.Config.PriorityBaseURL)
	client.SetHeaders(map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	})

	// Set basic auth
	client.SetBasicAuth(common.Config.PriorityUsername, common.Config.PriorityPassword)

	return &Client{client: client}
}

// GetCustomerByEmail fetches customer information by email from Priority ERP
// It searches for customers where EMAIL field matches the given email
func (c *Client) GetCustomerByEmail(ctx context.Context, email string) (*Customer, error) {
	// Build OData filter query
	filter := fmt.Sprintf("EMAIL eq '%s'", email)

	req := c.client.NewRequest()
	req.SetContext(ctx)

	// Build the query with OData parameters
	resp, err := req.
		SetQueryParam("$filter", filter).
		SetQueryParam("$top", "1"). // We only need the first match
		SetResult(&CustomerODataResponse{}).
		Get("CUSTOMERS")

	if err != nil {
		return nil, fmt.Errorf("priority client request failed: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return nil, nil // Return nil, not an error, if not found
		}
		return nil, fmt.Errorf("priority API error [%d]: %s", resp.StatusCode(), resp.String())
	}

	result := resp.Result().(*CustomerODataResponse)
	if result == nil || result.Value == nil || len(result.Value) == 0 {
		return nil, nil // No customer found
	}

	// Return the first matching customer
	return &result.Value[0], nil
}

// GetAccountReceivables fetches account receivables for a given customer ID from Priority ERP
// The API path is: /ACCOUNTS_RECEIVABLE('{customerID}')/ACCFNCITEMS2_SUBFORM
func (c *Client) GetAccountReceivables(ctx context.Context, customerID string) ([]AccountReceivableItem, error) {
	// Build the API path with the customer ID
	path := fmt.Sprintf("ACCOUNTS_RECEIVABLE('%s')/ACCFNCITEMS2_SUBFORM", customerID)

	req := c.client.NewRequest()
	req.SetContext(ctx)

	// Build the query
	resp, err := req.
		SetResult(&AccountReceivableODataResponse{}).
		Get(path)

	if err != nil {
		return nil, fmt.Errorf("priority client request failed: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return []AccountReceivableItem{}, nil // Return empty slice, not an error
		}
		return nil, fmt.Errorf("priority API error [%d]: %s", resp.StatusCode(), resp.String())
	}

	result := resp.Result().(*AccountReceivableODataResponse)
	if result == nil || result.Value == nil {
		return []AccountReceivableItem{}, nil
	}

	// Handle pagination if there's a next link
	allItems := result.Value
	nextLink := result.ODataNextLink

	for nextLink != "" {
		// Create a new request for the next page (nextLink is a full URL)
		nextReq := c.client.NewRequest()
		nextReq.SetContext(ctx)

		nextResp, err := nextReq.
			SetResult(&AccountReceivableODataResponse{}).
			Get(nextLink)

		if err != nil {
			// Log warning but return what we have
			return allItems, fmt.Errorf("error fetching next page (returning partial results): %w", err)
		}

		if nextResp.IsError() {
			// Log warning but return what we have
			return allItems, fmt.Errorf("error fetching next page [%d] (returning partial results): %s",
				nextResp.StatusCode(), nextResp.String())
		}

		nextResult := nextResp.Result().(*AccountReceivableODataResponse)
		if nextResult != nil && nextResult.Value != nil {
			allItems = append(allItems, nextResult.Value...)
			nextLink = nextResult.ODataNextLink
		} else {
			break
		}
	}

	return allItems, nil
}

func (c *Client) GetLastContributions(ctx context.Context, email string) (map[string]float64, error) {
	// 1. Fetch customer by email.
	customer, err := c.GetCustomerByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("c.GetCustomerByEmail: %w", err)
	}
	if customer == nil || customer.CustName == "" {
		return nil, fmt.Errorf("customer not found for email: %s", email)
	}

	// 2. Fetch all account receivables by customerID.
	accountReceivables, err := c.GetAccountReceivables(ctx, customer.CustName)
	if err != nil {
		return nil, fmt.Errorf("c.GetAccountReceivables: %w", err)
	}

	// 3. Filter last 12 months by FNCDATE, only ACCNAME == "40001", sum DEBIT per CODE (currency).
	now := time.Now()
	twelveMonthsAgo := now.AddDate(0, -12, 0)

	sums := make(map[string]float64)
	for _, item := range accountReceivables {
		// Filter by ACCNAME
		if item.ACCNAME != "40001" {
			continue
		}
		// Parse FNCDATE (example: "2025-09-12T00:00:00Z")
		fncDate, err := time.Parse(time.RFC3339, item.FNCDATE)
		if err != nil {
			// If date invalid, skip this record
			continue
		}
		// Only last 12 months
		if fncDate.Before(twelveMonthsAgo) {
			continue
		}
		// Sum DEBIT per CODE (currency)
		sums[item.CODE] += item.DEBIT
	}

	return sums, nil
}
