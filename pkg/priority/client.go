// Package priority provides a client for interacting with Priority ERP Cloud API.
package priority

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
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

// contributionACCNAMEFilter is the OData $filter clause matching contributionACCNAMEs, built once.
var contributionACCNAMEFilter = "(" + buildOrFilter("ACCNAME", sortedKeys(contributionACCNAMEs)) + ")"

const contributionCacheTTL = 30 * time.Minute

// Chunk sizes for GetLastContributionsBatch, chosen to keep OData $filter query strings
// well under typical gateway URL-length limits while still batching many customers per request.
const (
	batchEmailChunkSize    = 40
	batchCustNameChunkSize = 60
)

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

// recordStats adds one request's metrics into stats, if a non-nil collector was passed.
// stats is variadic at call sites so it stays an optional, backward-compatible parameter.
func recordStats(stats []*RequestStats, resp *resty.Response) {
	if len(stats) == 0 || stats[0] == nil {
		return
	}
	stats[0].Requests++
	stats[0].Bytes += len(resp.Body())
}

// GetCustomersByEmail fetches all customers matching the given email from Priority ERP.
// Returns an empty slice (no error) on 404 or empty result.
// An optional *RequestStats can be passed to accumulate request-count/byte diagnostics.
func (c *Client) GetCustomersByEmail(ctx context.Context, email string, stats ...*RequestStats) ([]Customer, error) {
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
	recordStats(stats, resp)

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
// An optional *RequestStats can be passed to accumulate request-count/byte diagnostics.
func (c *Client) GetActiveCustomersByEmail(ctx context.Context, email string, stats ...*RequestStats) ([]Customer, error) {
	customers, err := c.GetCustomersByEmail(ctx, email, stats...)
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
// An optional *RequestStats can be passed to accumulate request-count/byte diagnostics.
func (c *Client) GetAccountReceivables(ctx context.Context, customerID string, stats ...*RequestStats) ([]AccountReceivableItem, error) {
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
	recordStats(stats, resp)

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
		recordStats(stats, nextResp)

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

	sums, _, err := c.fetchLastContributions(ctx, email, nil)
	if err != nil {
		if errors.Is(err, ErrNoActiveCustomers) && c.contributionCache != nil {
			c.contributionCache.Put(cacheKey, contributionResult{noActiveCustomers: true})
		}
		return nil, err
	}

	if c.contributionCache != nil {
		c.contributionCache.Put(cacheKey, contributionResult{sums: sums})
	}
	return sums, nil
}

// GetLastContributionsWithStats behaves like GetLastContributions but also returns
// diagnostic request-count/byte/duration metrics, for comparison against
// GetLastContributionsBatch. It always hits Priority directly, bypassing the cache,
// so the numbers reflect real request traffic.
func (c *Client) GetLastContributionsWithStats(ctx context.Context, email string) (map[string]float64, RequestStats, error) {
	start := time.Now()
	sums, stats, err := c.fetchLastContributions(ctx, email, &RequestStats{})
	stats.Duration = time.Since(start)
	return sums, stats, err
}

// fetchLastContributions does the actual full-history-per-customer fetch behind
// GetLastContributions / GetLastContributionsWithStats. If stats is non-nil, request
// count and response bytes are accumulated into it.
func (c *Client) fetchLastContributions(ctx context.Context, email string, stats *RequestStats) (map[string]float64, RequestStats, error) {
	if stats == nil {
		stats = &RequestStats{}
	}

	// 1. Fetch active customers by email.
	activeCustomers, err := c.GetActiveCustomersByEmail(ctx, email, stats)
	if err != nil {
		return nil, *stats, fmt.Errorf("c.GetActiveCustomersByEmail: %w", err)
	}

	// Filter to customers with a usable CustName.
	usable := make([]Customer, 0, len(activeCustomers))
	for _, cust := range activeCustomers {
		if cust.CustName != "" {
			usable = append(usable, cust)
		}
	}
	if len(usable) == 0 {
		return nil, *stats, fmt.Errorf("%w: %s", ErrNoActiveCustomers, email)
	}

	// 2. For each active customer, fetch receivables and accumulate sums.
	now := time.Now()
	twelveMonthsAgo := now.AddDate(0, -12, 0)
	sums := make(map[string]float64)

	for _, customer := range usable {
		accountReceivables, err := c.GetAccountReceivables(ctx, customer.CustName, stats)
		if err != nil {
			return nil, *stats, fmt.Errorf("c.GetAccountReceivables: %w", err)
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

	return sums, *stats, nil
}

// buildOrFilter builds an OData `field eq 'v1' or field eq 'v2' ...` clause.
func buildOrFilter(field string, values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%s eq '%s'", field, strings.ReplaceAll(v, "'", "''"))
	}
	return strings.Join(parts, " or ")
}

// chunkStrings splits values into slices of at most size elements.
func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 {
		return nil
	}
	if size <= 0 || len(values) <= size {
		return [][]string{values}
	}
	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for i := 0; i < len(values); i += size {
		end := i + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[i:end])
	}
	return chunks
}

// sortedKeys returns the sorted keys of a string-keyed set, for deterministic filter strings.
func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// GetLastContributionsBatch fetches last-12-months DEBIT contribution sums (by ISO currency)
// for every Priority customer resolved from the given emails, in a small, fixed number of
// chunked requests regardless of how many emails/customers are involved. Unlike
// GetLastContributions (which does a full, unfiltered history fetch per customer), this
// pushes the date range, contribution-category filter, and field selection down to Priority
// via $filter/$select/$expand, so both the request count and the bytes transferred stay flat
// as the batch grows.
//
// The whole batch is fetched exactly once and returned keyed by customer (ContributionsBatchResult).
// Callers group the requested emails into whatever logical units they need (e.g. one group per
// person, who may have several email aliases) and call result.SumGroup(group) per group --
// purely an in-memory step, no further Priority calls.
//
// Phase 1: one CUSTOMERS request per chunk of emails (OR-filtered on EMAIL, $select limited
// to the fields needed for active-status filtering) resolves emails to CUSTNAMEs.
// Phase 2: one ACCOUNTS_RECEIVABLE request per chunk of the resulting deduped CUSTNAMEs
// (OR-filtered on ACCNAME, $select=ACCNAME, $expand=ACCFNCITEMS2_SUBFORM($select=...;
// $filter=last 12mo + contribution categories + DEBIT>0)) fetches only the relevant rows for
// all those customers at once.
func (c *Client) GetLastContributionsBatch(ctx context.Context, emails []string) (*ContributionsBatchResult, error) {
	start := time.Now()
	var stats RequestStats

	result := &ContributionsBatchResult{ByCustomer: make(map[string]map[string]float64)}

	uniqueEmails := make([]string, 0, len(emails))
	seen := make(map[string]bool, len(emails))
	for _, email := range emails {
		if email == "" {
			continue
		}
		key := strings.ToLower(email)
		if !seen[key] {
			seen[key] = true
			uniqueEmails = append(uniqueEmails, email)
		}
	}

	custNamesByEmail, err := c.resolveActiveCustNames(ctx, uniqueEmails, &stats)
	if err != nil {
		return nil, fmt.Errorf("c.resolveActiveCustNames: %w", err)
	}
	result.CustNamesByEmail = custNamesByEmail

	custNameSet := make(map[string]struct{})
	for _, custNames := range custNamesByEmail {
		for _, custName := range custNames {
			custNameSet[custName] = struct{}{}
		}
	}

	if len(custNameSet) == 0 {
		stats.Duration = time.Since(start)
		result.Stats = stats
		return result, nil
	}

	if err := c.fetchContributionsByCustomer(ctx, sortedKeys(custNameSet), result.ByCustomer, &stats); err != nil {
		return nil, fmt.Errorf("c.fetchContributionsByCustomer: %w", err)
	}

	stats.Duration = time.Since(start)
	result.Stats = stats
	return result, nil
}

// resolveActiveCustNames resolves emails to their active Priority CUSTNAMEs, chunking the
// EMAIL lookup so a single request covers many emails at once. Keyed by lower-cased email,
// since Priority's stored EMAIL value may differ in case from the caller's spelling.
func (c *Client) resolveActiveCustNames(ctx context.Context, emails []string, stats *RequestStats) (map[string][]string, error) {
	custNamesByEmail := make(map[string][]string)

	for _, chunk := range chunkStrings(emails, batchEmailChunkSize) {
		// Parens matter: "and" binds tighter than "or" in OData, so without them this would
		// apply these checks only to the last ORed email clause.
		// Mirrors IsActive() server-side. STATDES was confirmed (by sampling real data) to
		// only ever be exactly "פעיל" or "לא פעיל" -- a clean enum, not free text -- so a
		// plain "ne" works here instead of needing an unconfirmed contains() function.
		// IsActive() stays as a client-side check below regardless, as a safety net.
		filter := fmt.Sprintf(`(%s) and INACTIVEFLAG ne 'Y' and STATDES ne 'לא פעיל'`,
			buildOrFilter("EMAIL", chunk))

		path := "CUSTOMERS"
		useQueryParams := true
		for {
			req := c.client.NewRequest()
			req.SetContext(ctx)
			if useQueryParams {
				req.SetQueryParam("$filter", filter).
					SetQueryParam("$select", "CUSTNAME,EMAIL,STATDES,INACTIVEFLAG")
			}
			resp, err := req.SetResult(&CustomerODataResponse{}).Get(path)
			if err != nil {
				return nil, fmt.Errorf("priority client request failed: %w", err)
			}
			stats.Requests++
			stats.Bytes += len(resp.Body())

			if resp.IsError() {
				if resp.StatusCode() == http.StatusNotFound {
					break
				}
				return nil, fmt.Errorf("priority API error [%d]: %s", resp.StatusCode(), resp.String())
			}

			cr, _ := resp.Result().(*CustomerODataResponse)
			if cr == nil {
				break
			}
			for _, cust := range cr.Value {
				// CUSTNAME is the CUSTOMERS entity key (never empty) and EMAIL can't come
				// back empty since we only ever filter for exact non-empty values (see the
				// empty-string guard on uniqueEmails above) -- IsActive() is the only real check.
				if !cust.IsActive() {
					continue
				}
				key := strings.ToLower(cust.Email)
				custNamesByEmail[key] = append(custNamesByEmail[key], cust.CustName)
			}

			if cr.ODataNextLink == "" {
				break
			}
			path = cr.ODataNextLink
			useQueryParams = false
		}
	}

	return custNamesByEmail, nil
}

// fetchContributionsByCustomer fetches filtered/selected contribution items for custNames in
// chunks and adds each item's DEBIT (converted to ISO currency) into byCustomer, keyed by
// CUSTNAME.
func (c *Client) fetchContributionsByCustomer(ctx context.Context, custNames []string, byCustomer map[string]map[string]float64, stats *RequestStats) error {
	// No DEBIT filter here: the legacy fetch sums every ACCNAME-matching, in-range row
	// unconditionally (including negative-DEBIT reversal/correction rows), so the batch
	// query must fetch the same rows or its sum silently diverges from legacy's.
	//
	// cutoff is the exact same precise-instant cutoff GetLastContributions uses. FNCDATE is
	// a Priority "Date"-typed field, so the server compares it at whole-day granularity --
	// asking it for "ge cutoff" directly could include or exclude the entire boundary day
	// depending on server-side rounding we don't control. To match GetLastContributions
	// exactly regardless of that rounding, the server-side filter asks for one extra day
	// (serverCutoff) and the precise cutoff check is re-applied client-side per item below,
	// identically to GetLastContributions's own fncDate.Before(cutoff) check.
	cutoff := time.Now().AddDate(0, -12, 0)
	serverCutoff := cutoff.AddDate(0, 0, -1).UTC().Format("2006-01-02T15:04:05Z")
	itemFilter := fmt.Sprintf("FNCDATE ge %s and %s", serverCutoff, contributionACCNAMEFilter)
	expand := fmt.Sprintf("ACCFNCITEMS2_SUBFORM($select=ACCNAME,CODE,DEBIT,FNCDATE;$filter=%s)", itemFilter)

	for _, chunk := range chunkStrings(custNames, batchCustNameChunkSize) {
		outerFilter := buildOrFilter("ACCNAME", chunk)

		path := "ACCOUNTS_RECEIVABLE"
		useQueryParams := true
		for {
			req := c.client.NewRequest()
			req.SetContext(ctx)
			if useQueryParams {
				req.SetQueryParam("$filter", outerFilter).
					SetQueryParam("$select", "ACCNAME").
					SetQueryParam("$expand", expand)
			}
			resp, err := req.SetResult(&accountsReceivableExpandResponse{}).Get(path)
			if err != nil {
				return fmt.Errorf("priority client request failed: %w", err)
			}
			stats.Requests++
			stats.Bytes += len(resp.Body())

			if resp.IsError() {
				if resp.StatusCode() == http.StatusNotFound {
					break
				}
				return fmt.Errorf("priority API error [%d]: %s", resp.StatusCode(), resp.String())
			}

			ar, _ := resp.Result().(*accountsReceivableExpandResponse)
			if ar == nil {
				break
			}
			for _, acc := range ar.Value {
				custSums := byCustomer[acc.ACCNAME]
				if custSums == nil {
					custSums = make(map[string]float64)
					byCustomer[acc.ACCNAME] = custSums
				}
				for _, item := range acc.Items {
					fncDate, err := time.Parse(time.RFC3339, item.FNCDATE)
					if err != nil {
						continue
					}
					if fncDate.Before(cutoff) {
						continue
					}
					iso, ok := currencyCodeMap[item.CODE]
					if !ok {
						iso = common.CurrencyNIS
					}
					custSums[iso] += item.DEBIT
				}
			}

			if ar.ODataNextLink == "" {
				break
			}
			path = ar.ODataNextLink
			useQueryParams = false
		}
	}

	return nil
}
