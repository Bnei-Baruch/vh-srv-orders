package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getWith answers one request with the given status, content type and body,
// through a client set up the way accounting and profiles set theirs up.
func getWith(t *testing.T, status int, contentType, body string) *resty.Response {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	resp, err := resty.New().SetError(APIError{}).R().Get(server.URL)
	require.NoError(t, err)
	return resp
}

func TestRespError_SuccessIsNil(t *testing.T) {
	resp := getWith(t, http.StatusOK, "application/json", `{"ok":true}`)

	assert.NoError(t, RespError(resp))
}

func TestRespError_UsesTheServiceMessage(t *testing.T) {
	resp := getWith(t, http.StatusInternalServerError, "application/json", `{"error":"db is down"}`)

	assert.EqualError(t, RespError(resp), "db is down")
}

// A JSON body without "error" must not become errors.New("").
func TestRespError_JSONWithoutErrorFieldFallsBack(t *testing.T) {
	resp := getWith(t, http.StatusInternalServerError, "application/json", `{"message":"boom"}`)

	assert.EqualError(t, RespError(resp), `unexpected response: [500 Internal Server Error] {"message":"boom"}`)
}

// resty decodes the error body only for JSON or XML, so a proxy's HTML page
// never reaches APIError.
func TestRespError_NonJSONFallsBack(t *testing.T) {
	resp := getWith(t, http.StatusBadGateway, "text/html", "<html>502</html>")

	assert.EqualError(t, RespError(resp), "unexpected response: [502 Bad Gateway] <html>502</html>")
}

func TestRespStatusError_SuccessIsNil(t *testing.T) {
	resp := getWith(t, http.StatusOK, "application/json", `{"value":[]}`)

	assert.NoError(t, RespStatusError(resp, "priority API error"))
}

// The message keeps the shape the Priority and Pelecard clients built inline.
func TestRespStatusError_NamesTheAPIStatusAndBody(t *testing.T) {
	resp := getWith(t, http.StatusServiceUnavailable, "text/plain", "maintenance")

	assert.EqualError(t, RespStatusError(resp, "priority API error"), "priority API error [503]: maintenance")
}
