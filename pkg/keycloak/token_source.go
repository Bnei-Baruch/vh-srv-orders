package keycloak

import (
	"errors"
	"fmt"
	"strings"
)

type TokenSource interface {
	Token() (string, error)
}

// AuthHeaderTokenSource will strip the token from the header and reuse it forever
func AuthHeaderTokenSource(authHeader string) TokenSource {
	parts := strings.Split(authHeader, " ")
	if len(parts) == 0 {
		return authHeaderTokenSource{token: "", err: errors.New("missing auth header")}
	}
	if len(parts) == 1 || parts[1] == "" {
		return authHeaderTokenSource{token: "", err: fmt.Errorf("malformed auth header: %s", authHeader)}
	}
	return authHeaderTokenSource{token: parts[1], err: nil}
}

type authHeaderTokenSource struct {
	token string
	err   error
}

// StaticTokenSource will simply use the same token over and over again.
// Main use case is when we proxy a token given in api calls to downstream services.
func StaticTokenSource(token string) TokenSource {
	return staticTokenSource{token: token}
}

type staticTokenSource struct {
	token string
}

func (s staticTokenSource) Token() (string, error) {
	return s.token, nil
}

func (s authHeaderTokenSource) Token() (string, error) {
	return s.token, s.err
}
