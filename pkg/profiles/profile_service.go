package profiles

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-resty/resty/v2"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
)

type ProfileService interface {
	LookupProfile(ctx context.Context, email string) (*Profile, error)
	LookupProfileByKeycloakId(ctx context.Context, keycloakId string) (*Profile, error)
	GetProfileByKeycloakID(ctx context.Context, keycloakId string) (*Profile, error)
}

type ProfileServiceAPI struct {
	client      *resty.Client
	tokenSource keycloak.TokenSource
}

func NewProfileServiceAPI(tokenSource keycloak.TokenSource) *ProfileServiceAPI {
	client := resty.New()
	client.SetBaseURL(common.Config.ProfileServiceUrl)
	client.SetHeaders(map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   common.ServiceName,
	})
	// TODO (edo): under API context we should propagate request ID (tracing) and actor (user-agent)

	client.SetError(APIError{})
	//client.EnableTrace()

	return &ProfileServiceAPI{
		client:      client,
		tokenSource: tokenSource,
	}
}

func (p *ProfileServiceAPI) LookupProfile(ctx context.Context, email string) (*Profile, error) {
	resp, err := p.executeWithRetry(ctx, func(req *resty.Request) (*resty.Response, error) {
		return req.
			SetQueryParam("email", email).
			SetResult([]Profile{}).
			Get("/v1/profiles")
	})
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return nil, nil
		}
		return nil, respError(resp)
	}

	results := resp.Result().(*[]Profile)
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	return &(*results)[0], nil
}

func (p *ProfileServiceAPI) LookupProfileByKeycloakId(ctx context.Context, keycloakId string) (*Profile, error) {
	path := fmt.Sprintf("/v1/profile/%s", keycloakId)

	resp, err := p.executeWithRetry(ctx, func(req *resty.Request) (*resty.Response, error) {
		return req.
			SetResult(&Profile{}).
			Get(path)
	})
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return nil, nil
		}
		return nil, respError(resp)
	}

	result := resp.Result().(*Profile)
	if result == nil {
		return nil, nil
	}

	return result, nil
}

func (p *ProfileServiceAPI) GetProfileByKeycloakID(ctx context.Context, keycloakId string) (*Profile, error) {
	path := fmt.Sprintf("/v1/profile/%s", keycloakId)

	resp, err := p.executeWithRetry(ctx, func(req *resty.Request) (*resty.Response, error) {
		return req.
			SetResult(&Profile{}).
			Get(path)
	})
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		if resp.StatusCode() == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, respError(resp)
	}

	return resp.Result().(*Profile), nil
}

func (p *ProfileServiceAPI) baseRequest(ctx context.Context) (*resty.Request, error) {
	r := p.client.NewRequest()
	r.SetContext(ctx)

	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("tokenSource.Token(): %w", err)
	}
	r.SetAuthToken(token)

	return r, nil
}

// invalidateTokenSource clears token cache if TokenSource supports it (e.g., Client)
func (p *ProfileServiceAPI) invalidateTokenSource() {
	p.tokenSource.Invalidate()
}

// executeWithRetry executes a request and retries once on 401 after invalidating the token source
func (p *ProfileServiceAPI) executeWithRetry(ctx context.Context,
	execute func(*resty.Request) (*resty.Response, error)) (*resty.Response, error) {

	// First attempt
	req, err := p.baseRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("baseRequest: %w", err)
	}

	resp, err := execute(req)
	if err != nil {
		return nil, err
	}

	// If 401, invalidate token cache and retry once
	if resp != nil && resp.StatusCode() == http.StatusUnauthorized {
		p.invalidateTokenSource()

		// Get fresh token and retry
		req, err := p.baseRequest(ctx)
		if err != nil {
			return nil, fmt.Errorf("baseRequest (retry): %w", err)
		}

		resp, err = execute(req)
		if err != nil {
			return nil, err
		}

		// If still 401 after retry with fresh token, return the error
		if resp != nil && resp.StatusCode() == http.StatusUnauthorized {
			return nil, respError(resp)
		}
	}

	return resp, nil
}

func respError(resp *resty.Response) error {
	if resp.IsError() {
		if apiErr, ok := resp.Error().(*APIError); ok {
			return errors.New(apiErr.Error)
		} else {
			return fmt.Errorf("unexpected response: [%s] %s", resp.Status(), resp.String())
		}
	}
	return nil
}
