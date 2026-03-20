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

// GetCustomersByEmail fetches all customers matching the given email from Priority ERP.
// Returns an empty slice (no error) on 404 or empty result.
func (c *Client) GetCustomersByEmail(ctx context.Context, email string) ([]Customer, error) {
	filter := fmt.Sprintf("EMAIL eq '%s'", email)

	req := c.client.NewRequest()
	req.SetContext(ctx)

	resp, err := req.
		SetQueryParam("$filter", filter).
		SetResult(&CustomerODataResponse{}).
		Get("CUSTOMERS")

	if err != nil {
		return nil, fmt.Errorf("priority client request failed: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return []Customer{}, nil
		}
		return nil, fmt.Errorf("priority API error [%d]: %s", resp.StatusCode(), resp.String())
	}

	result := resp.Result().(*CustomerODataResponse)
	if result == nil || result.Value == nil {
		return []Customer{}, nil
	}

	return result.Value, nil
}

// GetActiveCustomersByEmail returns only active customers for the given email.
func (c *Client) GetActiveCustomersByEmail(ctx context.Context, email string) ([]Customer, error) {
	customers, err := c.GetCustomersByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	active := make([]Customer, 0, len(customers))
	for _, customer := range customers {
		if customer.IsActive() {
			active = append(active, customer)
		}
	}
	return active, nil
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
	// 1. Fetch active customers by email.
	activeCustomers, err := c.GetActiveCustomersByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("c.GetActiveCustomersByEmail: %w", err)
	}

	// Filter to customers with a usable CustName.
	usable := make([]Customer, 0, len(activeCustomers))
	for _, cust := range activeCustomers {
		if cust.CustName != "" {
			usable = append(usable, cust)
		}
	}
	if len(usable) == 0 {
		return nil, fmt.Errorf("no active customers found for email: %s", email)
	}

	// 2. For each active customer, fetch receivables and accumulate sums.
	now := time.Now()
	twelveMonthsAgo := now.AddDate(0, -12, 0)
	sums := make(map[string]float64)

	for _, customer := range usable {
		accountReceivables, err := c.GetAccountReceivables(ctx, customer.CustName)
		if err != nil {
			return nil, fmt.Errorf("c.GetAccountReceivables: %w", err)
		}

		for _, item := range accountReceivables {
			if item.ACCNAME != "40001" {
				continue
			}
			fncDate, err := time.Parse(time.RFC3339, item.FNCDATE)
			if err != nil {
				continue
			}
			if fncDate.Before(twelveMonthsAgo) {
				continue
			}
			sums[item.CODE] += item.DEBIT
		}
	}

	return sums, nil
}
