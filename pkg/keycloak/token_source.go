package keycloak

type TokenSource interface {
	Token() (string, error)
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
