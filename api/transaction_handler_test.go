package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// captureRequestLog records what the logging middleware writes through
// slog.Default().
func captureRequestLog(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	real := slog.Default()
	t.Cleanup(func() { slog.SetDefault(real) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	return &buf
}

// requestErrors returns the errs of the logged "request" record. They are what
// the Sentry middleware captures on a 5xx.
func requestErrors(t *testing.T, log *bytes.Buffer) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["msg"] == "request" {
			assert.Equal(t, "ERROR", rec["level"])
			errs, _ := json.Marshal(rec["errs"])
			return string(errs)
		}
	}
	t.Fatal("no request record logged")
	return ""
}

// When the checkout service answers with something that isn't JSON, the
// caller must get a 500, not a 200 with a null body that looks like success,
// and the cause must be recorded on the request so it reaches Sentry.
func Test_new_token_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubUpstreamWithHTML(t)
	log := captureRequestLog(t)

	POST_ROOT(t, a, "/v2/transaction/new_token", map[string]any{}, http.StatusInternalServerError)

	assert.Contains(t, requestErrors(t, log), "getToken response json.Unmarshal")
}

func Test_new_token_no_cvv_non_json_upstream_is_500(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)
	stubUpstreamWithHTML(t)
	log := captureRequestLog(t)

	POST_ROOT(t, a, "/v2/transaction/new_token_no_cvv", map[string]any{}, http.StatusInternalServerError)

	assert.Contains(t, requestErrors(t, log), "getToken response json.Unmarshal")
}
