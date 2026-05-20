// Package priority provides a client for interacting with Priority ERP Cloud API.
package priority

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// ErrNoActiveCustomers is returned when no active Priority customers are found for an email.
var ErrNoActiveCustomers = errors.New("no active customers found")

// currencyCodeMap maps Priority ERP CODE values to ISO currency codes.
// Priority returns Hebrew abbreviations (e.g. ש"ח for NIS) — callers expect ISO.
// Unknown codes fall back to NIS (Priority's internal currency) with a warning log;
// this matches observed behavior where all donations come back as ש"ח regardless of
// donor's payment currency. Add entries as new codes are confirmed in live data.
var currencyCodeMap = map[string]string{
	`ש"ח`: common.CurrencyNIS, // Israeli shekel (Priority's native encoding)
	"NIS": common.CurrencyNIS,
	"ILS": common.CurrencyNIS, // ISO 4217 code for shekel
	"USD": common.CurrencyUSD,
	"EUR": common.CurrencyEUR,
}

// contributionACCNAMEs is the set of Priority ACCNAME codes that count as valid contributions.
var contributionACCNAMEs = map[string]struct{}{
	"40001": {}, // Donations
	"40002": {}, // Donations - Archive Project / The Connection Between Us
	"40004": {}, // Donations - MAK Course (Russian/Russia)
	"40038": {}, // Donations - Torah Lessons and Jewish Culture
	"40049": {}, // Donations - Learning Center (Spanish)
	"40050": {}, // Donations - Help-Haver
	"40053": {}, // Donations - Building Loan
	"40054": {}, // Donations - Spanish
	"40061": {}, // Donations - Visitors Center
	"40100": {}, // Donations - Asia and Africa
}

const contributionCacheTTL = 30 * time.Minute

// contributionResult caches both success and "no active customers" outcomes.
type contributionResult struct {
	sums              map[string]float64
	noActiveCustomers bool
}

// Client is a client for interacting with Priority ERP Cloud API
type Client struct {
	client            *resty.Client
	contributionCache *utils.TTLCache[string, contributionResult]
}

// NewClient creates a new Priority ERP client with basic authentication.
// Contribution cache is disabled by default — call SetCacheEnabled(true) to enable.
func NewClient() *Client {
	client := resty.New()
	client.SetBaseURL(common.Config.PriorityBaseURL)
	client.SetTimeout(30 * time.Second)
	client.SetHeaders(map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	})

	// Set basic auth
	client.SetBasicAuth(common.Config.PriorityUsername, common.Config.PriorityPassword)

	return &Client{client: client}
}

// SetCacheEnabled enables or disables the contribution cache.
// Disabling clears any cached data. Enabling creates a fresh empty cache.
func (c *Client) SetCacheEnabled(enabled bool) {
	if enabled {
		c.contributionCache = utils.NewTTLCache[string, contributionResult](contributionCacheTTL)
	} else {
		c.contributionCache = nil
	}
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

// GetCustomerByID fetches a single customer from Priority ERP by CUSTNAME (customer code).
// Returns (nil, nil) on 404.
func (c *Client) GetCustomerByID(ctx context.Context, customerID string) (*Customer, error) {
	path := fmt.Sprintf("CUSTOMERS('%s')", customerID)

	req := c.client.NewRequest()
	req.SetContext(ctx)

	resp, err := req.
		SetResult(&Customer{}).
		Get(path)

	if err != nil {
		return nil, fmt.Errorf("priority client request failed: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("priority API error [%d]: %s", resp.StatusCode(), resp.String())
	}

	customer, _ := resp.Result().(*Customer)
	if customer == nil || customer.CustName == "" {
		return nil, nil
	}
	return customer, nil
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
	cacheKey := strings.ToLower(email)

	// Check cache
	if c.contributionCache != nil {
		if cached, ok := c.contributionCache.Get(cacheKey); ok {
			if cached.noActiveCustomers {
				return nil, fmt.Errorf("%w: %s", ErrNoActiveCustomers, email)
			}
			slog.DebugContext(ctx, "contribution cache hit", slog.String("email", email))
			return cached.sums, nil
		}
	}

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
		if c.contributionCache != nil {
			c.contributionCache.Put(cacheKey, contributionResult{noActiveCustomers: true})
		}
		return nil, fmt.Errorf("%w: %s", ErrNoActiveCustomers, email)
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
			if _, ok := contributionACCNAMEs[item.ACCNAME]; !ok {
				continue
			}
			fncDate, err := time.Parse(time.RFC3339, item.FNCDATE)
			if err != nil {
				continue
			}
			if fncDate.Before(twelveMonthsAgo) {
				continue
			}
			iso, ok := currencyCodeMap[item.CODE]
			if !ok {
				utils.LogFor(ctx).Warn("unknown priority currency code, treating as NIS",
					slog.String("code", item.CODE),
					slog.String("cust_name", customer.CustName),
					slog.String("fnc_num", item.FNCNUM))
				iso = common.CurrencyNIS
			}
			sums[iso] += item.DEBIT
		}
	}

	if c.contributionCache != nil {
		c.contributionCache.Put(cacheKey, contributionResult{sums: sums})
	}
	return sums, nil
}
