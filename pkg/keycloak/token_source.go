package keycloak

import (
	"errors"
	"fmt"
	"strings"
)

type TokenSource interface {
	Token() (string, error)
	Invalidate()
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
//
// Nothing constructs it, and nothing has since it was added in 2023. Kept, but
// not for the reason the line above suggests: that proxying is already supplied.
// middleware.TokenSource builds an AuthHeaderTokenSource from the caller's
// Authorization header on every request and stores it under
// common.CtxTokenSource. What is missing is a reader — that value has two
// writers and, outside one test assertion, no consumer at all, so no downstream
// call authenticates as the caller today.
//
// So this is a second way to supply something already supplied. Whether it earns
// its place depends on which shape the consuming side ends up wanting, which is
// not decided.
func StaticTokenSource(token string) TokenSource {
	return staticTokenSource{token: token}
}

type staticTokenSource struct {
	token string
}

func (s staticTokenSource) Token() (string, error) {
	return s.token, nil
}

func (s staticTokenSource) Invalidate() {
	// No-op: StaticTokenSource uses a static token that cannot be invalidated
}

func (s authHeaderTokenSource) Token() (string, error) {
	return s.token, s.err
}

func (s authHeaderTokenSource) Invalidate() {
	// No-op: AuthHeaderTokenSource uses a static token that cannot be invalidated
}
