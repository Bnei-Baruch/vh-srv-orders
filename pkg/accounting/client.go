// Package accounting provides a client for the vh-srv-accounting service.
// Exposes contributions aggregated from QuickBooks and from the European
// donations system. Priority is expected to migrate behind this service in
// the future.
package accounting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

const contributionCacheTTL = 30 * time.Minute

type AccountingService interface {
	GetLastContributions(ctx context.Context, email string, companyID *string) (*ContributionsResult, error)
	GetEuropeContributions(ctx context.Context, emails []string) (*EuropeContributionsResult, error)
}

type AccountingServiceAPI struct {
	client                  *resty.Client
	tokenSource             keycloak.TokenSource
	contributionCache       *utils.TTLCache[string, ContributionsResult]
	europeContributionCache *utils.TTLCache[string, EuropeContributionsResult]
}

// NewAccountingServiceAPI creates a new client. Contribution cache is disabled
// by default — call SetCacheEnabled(true) to enable.
func NewAccountingServiceAPI(tokenSource keycloak.TokenSource) *AccountingServiceAPI {
	client := resty.New()
	client.SetBaseURL(common.Config.AccountingServiceUrl)
	client.SetHeaders(map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   common.ServiceName,
	})

	client.SetError(APIError{})

	return &AccountingServiceAPI{
		client:      client,
		tokenSource: tokenSource,
	}
}

// SetCacheEnabled enables or disables the contribution caches.
// Disabling clears any cached data; enabling creates fresh empty caches.
func (a *AccountingServiceAPI) SetCacheEnabled(enabled bool) {
	if enabled {
		a.contributionCache = utils.NewTTLCache[string, ContributionsResult](contributionCacheTTL)
		a.europeContributionCache = utils.NewTTLCache[string, EuropeContributionsResult](contributionCacheTTL)
	} else {
		a.contributionCache = nil
		a.europeContributionCache = nil
	}
}

// GetLastContributions returns the contributions breakdown for the given email
// over the last 12 months. A nil companyID aggregates across all enabled QuickBooks
// companies; a non-nil value scopes the query to that single company.
// Result.Found is false when the email did not match any customer.
func (a *AccountingServiceAPI) GetLastContributions(ctx context.Context, email string, companyID *string) (*ContributionsResult, error) {
	cacheKey := contributionCacheKey(email, companyID)
	if a.contributionCache != nil {
		if cached, ok := a.contributionCache.Get(cacheKey); ok {
			result := cached
			return &result, nil
		}
	}

	resp, err := a.executeWithRetry(ctx, func(req *resty.Request) (*resty.Response, error) {
		req.SetQueryParam("email", email).SetResult(&contributionsResponse{})
		if companyID != nil {
			req.SetQueryParam("company_id", *companyID)
		}
		return req.Get("/v1/quickbooks/contributions")
	})
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, respError(resp)
	}

	result := resp.Result().(*contributionsResponse)
	if result == nil || result.Data == nil {
		return nil, fmt.Errorf("malformed response: missing data")
	}

	if a.contributionCache != nil {
		a.contributionCache.Put(cacheKey, *result.Data)
	}
	return result.Data, nil
}

func contributionCacheKey(email string, companyID *string) string {
	key := strings.ToLower(email) + "|"
	if companyID != nil {
		key += *companyID
	}
	return key
}

// GetEuropeContributions returns per-email contribution sums from the European
// donations system over the last 12 months, in a single batch call.
// Per-entry Found is false when an email matched no customer; amounts have
// refunds subtracted upstream and may be negative.
func (a *AccountingServiceAPI) GetEuropeContributions(ctx context.Context, emails []string) (*EuropeContributionsResult, error) {
	if len(emails) == 0 {
		return &EuropeContributionsResult{}, nil
	}

	cacheKey := europeContributionsCacheKey(emails)
	if a.europeContributionCache != nil {
		if cached, ok := a.europeContributionCache.Get(cacheKey); ok {
			result := cached
			return &result, nil
		}
	}

	resp, err := a.executeWithRetry(ctx, func(req *resty.Request) (*resty.Response, error) {
		req.SetBody(europeContributionsRequest{Emails: emails}).SetResult(&europeContributionsResponse{})
		return req.Post("/v1/europe/contributions/batch")
	})
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, respError(resp)
	}

	result := resp.Result().(*europeContributionsResponse)
	if result == nil || result.Data == nil {
		return nil, fmt.Errorf("malformed response: missing data")
	}

	if a.europeContributionCache != nil {
		a.europeContributionCache.Put(cacheKey, *result.Data)
	}
	return result.Data, nil
}

// europeContributionsCacheKey derives a key from the sorted, lowercased email set.
func europeContributionsCacheKey(emails []string) string {
	keys := make([]string, len(emails))
	for i, email := range emails {
		keys[i] = strings.ToLower(email)
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

func (a *AccountingServiceAPI) baseRequest(ctx context.Context) (*resty.Request, error) {
	r := a.client.NewRequest()
	r.SetContext(ctx)

	token, err := a.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("tokenSource.Token(): %w", err)
	}
	r.SetAuthToken(token)

	return r, nil
}

func (a *AccountingServiceAPI) invalidateTokenSource() {
	a.tokenSource.Invalidate()
}

// executeWithRetry executes a request and retries once on 401 after invalidating the token source.
func (a *AccountingServiceAPI) executeWithRetry(ctx context.Context,
	execute func(*resty.Request) (*resty.Response, error)) (*resty.Response, error) {

	req, err := a.baseRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("baseRequest: %w", err)
	}

	resp, err := execute(req)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode() == http.StatusUnauthorized {
		a.invalidateTokenSource()

		req, err := a.baseRequest(ctx)
		if err != nil {
			return nil, fmt.Errorf("baseRequest (retry): %w", err)
		}

		resp, err = execute(req)
		if err != nil {
			return nil, err
		}

		if resp != nil && resp.StatusCode() == http.StatusUnauthorized {
			return nil, respError(resp)
		}
	}

	return resp, nil
}

func respError(resp *resty.Response) error {
	if resp.IsError() {
		if apiErr, ok := resp.Error().(*APIError); ok && apiErr.Error != "" {
			return errors.New(apiErr.Error)
		}
		return fmt.Errorf("unexpected response: [%s] %s", resp.Status(), resp.String())
	}
	return nil
}
