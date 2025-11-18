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
	req, err := p.baseRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("p.baseRequest: %w", err)
	}

	resp, err := req.
		SetQueryParam("email", email).
		SetResult([]Profile{}).
		Get("/v1/profiles")
	if err != nil {
		return nil, fmt.Errorf("req.Get: %w", err)
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
	req, err := p.baseRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("p.baseRequest: %w", err)
	}

	path := fmt.Sprintf("/v1/profile/%s", keycloakId)

	resp, err := req.
		SetResult(&Profile{}).
		Get(path)
	if err != nil {
		return nil, fmt.Errorf("req.Get: %w", err)
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
	req, err := p.baseRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("p.baseRequest: %w", err)
	}

	path := fmt.Sprintf("/v1/profile/%s", keycloakId)

	resp, err := req.
		SetResult(&Profile{}).
		Get(path)
	if err != nil {
		return nil, fmt.Errorf("req.Get: %w", err)
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
