package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
)

// htmlUpstream stands in for the checkout service answering with a non-JSON
// page (a proxy error, a maintenance page). utils.PostJSON uses a zero
// http.Client, which goes through http.DefaultTransport.
type htmlUpstream struct{}

func (htmlUpstream) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadGateway,
		Status:     "502 Bad Gateway",
		Header:     http.Header{"Content-Type": {"text/html"}},
		Body:       io.NopCloser(strings.NewReader("<html>502 Bad Gateway</html>")),
		Request:    r,
	}, nil
}

func stubUpstreamWithHTML(t *testing.T) {
	real := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = real })
	http.DefaultTransport = htmlUpstream{}
}

// postAsRoot sends a raw request, without do()'s JSON decoding: the response
// under test has no body.
func postAsRoot(a *App, path string, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	claims := &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{common.RoleRoot}},
	}
	a.gEngine.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), common.CtxAuthClaims, claims)))
	return w
}

// When the checkout service answers with something that isn't JSON, the
// caller must get a 500, not a 200 with a null body that looks like success.
func Test_new_token_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubUpstreamWithHTML(t)

	w := postAsRoot(a, "/v2/transaction/new_token", `{}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.NotEqual(t, "null", w.Body.String())
}

func Test_new_token_no_cvv_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubUpstreamWithHTML(t)

	w := postAsRoot(a, "/v2/transaction/new_token_no_cvv", `{}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.NotEqual(t, "null", w.Body.String())
}
