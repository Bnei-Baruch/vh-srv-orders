package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

const USER_KEY = "keycloak123"

func NewTestApp(t *testing.T) *App {
	gin.SetMode(gin.TestMode)
	a := NewApp()
	a.SetEmitter(new(events.NoopEmitter))

	var err error
	a.repo, err = testutil.NewTestOrdersDB(t, context.Background(), a.eventEmitter)
	require.Nil(t, err)

	a.ordersAPI = NewOrdersAPI(a.repo)
	a.gEngine = gin.Default()
	a.gEngine.Use(
		middleware.Logging(),
		middleware.EventsBuilder(),
	)
	a.initRoutes()
	return a
}

func CloseTestApp(a *App) {
	a.Shutdown()
}

func requestToMultipart(t *testing.T, request interface{}, files []AttachedFile) (io.Reader, string) {
	// Round-trip via JSON so json tags/omitempty/null.* are respected.
	b, err := json.Marshal(request)
	require.NoError(t, err, "requestToMultipart json.Marshal")
	var m map[string]any
	err = json.Unmarshal(b, &m)
	require.NoError(t, err, "requestToMultipart json.Unmarshal")

	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	for k, v := range m {
		if v == nil {
			continue // skip nulls
		}
		switch val := v.(type) {
		case string:
			err = w.WriteField(k, val)
			require.NoError(t, err, "requestToMultipart WriteField string")
		case float64, float32, int, int64, int32, uint, uint64, bool:
			err = w.WriteField(k, fmt.Sprint(val))
			require.NoError(t, err, "requestToMultipart WriteField number")
		default:
			// Fallback: encode nested object/array as JSON string
			j, e := json.Marshal(val)
			require.NoError(t, e, "requestToMultipart nested marshal")
			err = w.WriteField(k, string(j))
			require.NoError(t, err, "requestToMultipart nested WriteField")
		}
	}

	for _, f := range files {
		part, err := w.CreateFormFile(f.Field, f.Filename)
		require.NoError(t, err, "requestToMultipart file %q", f.Field)
		_, err = part.Write(f.Bytes)
		require.NoError(t, err, "requestToMultipart  write %q", f.Field)
	}

	err = w.Close()
	require.NoError(t, err, "requestToMultipart close")

	return buf, w.FormDataContentType()
}

func GET(t *testing.T, a *App, path string, expectedCode int) gin.H {
	return do(t, a, "GET", path, nil, expectedCode, DoOptions{})
}

func POST_ROOT(t *testing.T, a *App, path string, request interface{}, expectedCode int) gin.H {
	return do(t, a, "POST", path, request, expectedCode, DoOptions{isRoot: true})
}

func POST(t *testing.T, a *App, path string, request interface{}, expectedCode int) gin.H {
	return do(t, a, "POST", path, request, expectedCode, DoOptions{})
}

func PATCH_ROOT(t *testing.T, a *App, path string, request interface{}, expectedCode int) gin.H {
	return do(t, a, "PATCH", path, request, expectedCode, DoOptions{isRoot: true})
}

func PATCH_ROOT_MULTIPART(t *testing.T, a *App, path string, request interface{}, files []AttachedFile, expectedCode int) gin.H {
	return do(t, a, "PATCH", path, request, expectedCode, DoOptions{isRoot: true, isMultipart: true, files: files})
}

// Attachement file like receipt.
type AttachedFile struct {
	Field    string // form field name (e.g., "receipt")
	Filename string // presented filename
	Bytes    []byte // content
}

type DoOptions struct {
	isRoot      bool
	isMultipart bool

	// Applicable only when isMultipart is true.
	files []AttachedFile
}

func do(t *testing.T, a *App, method string, path string, request interface{}, expectedCode int, o DoOptions) gin.H {
	w := httptest.NewRecorder()

	var body io.Reader
	var content_type string
	if o.isMultipart {
		body, content_type = requestToMultipart(t, request, o.files)
	} else if request != nil {
		reqJsonValue, err := json.Marshal(request)
		require.NoError(t, err, "request json.Marshal")
		body = bytes.NewBuffer(reqJsonValue)
	}
	r := httptest.NewRequest(method, path, body)
	if o.isMultipart {
		r.Header.Set("Content-Type", content_type)
	}
	ctx := r.Context()
	role := "some-role" // user
	if o.isRoot {
		role = common.RoleRoot
	}
	claims := &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{role}},
	}
	ctx = context.WithValue(ctx, common.CtxAuthClaims, claims)
	a.gEngine.ServeHTTP(w, r.WithContext(ctx))
	require.Equal(t, expectedCode, w.Code, w.Body.String())

	var payload gin.H
	b := w.Body.Bytes()
	err := json.Unmarshal(b, &payload)
	require.NoError(t, err, "json.Unmarshal")
	return payload
}
