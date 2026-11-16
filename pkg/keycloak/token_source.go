package keycloak

import (
	"errors"
	"fmt"
	"strings"
)

type TokenSource interface {
	Token() (string, error)
	Invalidate()
	// InvalidateToken drops stale only if it is still the cached token, so a
	// caller reacting to a 401 cannot discard a token another caller just
	// replaced.
	InvalidateToken(stale string)
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
// Unused since it was added in 2023, and kept on purpose. One thing to know
// before reaching for it: the proxying described above is already supplied.
// middleware.TokenSource puts an AuthHeaderTokenSource under
// common.CtxTokenSource on every request; what is missing is a reader for it —
// two writers, no consumer outside one test assertion. So this is the second
// supplier, and it is the one to use if the consuming side turns out to want a
// token rather than a header.
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

func (s staticTokenSource) InvalidateToken(string) {
	// No-op, as Invalidate.
}

func (s authHeaderTokenSource) Token() (string, error) {
	return s.token, s.err
}

func (s authHeaderTokenSource) Invalidate() {
	// No-op: AuthHeaderTokenSource uses a static token that cannot be invalidated
}

func (s authHeaderTokenSource) InvalidateToken(string) {
	// No-op, as Invalidate.
}
