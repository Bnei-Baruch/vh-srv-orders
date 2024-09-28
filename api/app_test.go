package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
)

const USER_KEY = "keycloak123"

func NewTestApp(t *testing.T) *App {
	gin.SetMode(gin.TestMode)
	a := NewApp()
	a.initEventEmitter(true)

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

func NewRequestAsUser(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	ctx := r.Context()
	claims := &middleware.IDTokenClaims{
		Sub:         USER_KEY,
		RealmAccess: middleware.Roles{Roles: []string{"something"}},
	}
	ctx = context.WithValue(ctx, common.CtxAuthClaims, claims)
	return r.WithContext(ctx)
}

func GET(t *testing.T, a *App, path string, expectedCode int) gin.H {
	w := httptest.NewRecorder()
	a.gEngine.ServeHTTP(w, NewRequestAsUser("GET", path, nil))
	require.Equal(t, expectedCode, w.Code)

	var payload gin.H
  err := json.Unmarshal(w.Body.Bytes(), &payload)
	require.NoError(t, err, "GET json.Unmarshal")
	return payload
}

func POST(t *testing.T, a *App, path string, request interface{}, expectedCode int) gin.H {
	accountReqJsonValue, err := json.Marshal(request)
	require.NoError(t, err, "POST json.Marshal")
	w := httptest.NewRecorder()
	a.gEngine.ServeHTTP(w, NewRequestAsUser("POST", path, bytes.NewBuffer(accountReqJsonValue)))
	require.Equal(t, expectedCode, w.Code)

	var payload gin.H
	err = json.Unmarshal(w.Body.Bytes(), &payload)
	require.NoError(t, err, "POST json.Unmarshal")
	return payload
}
