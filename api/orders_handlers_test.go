package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/peterldowns/pgtestdb"
	"github.com/peterldowns/pgtestdb/migrators/golangmigrator"
	"github.com/stretchr/testify/assert"
	// "github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/api/middleware"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

const USER_KEY = "keycloak123"

func NewTestApp(t *testing.T) (*App, context.Context) {
  a := NewApp()
  em, err := events.CreateEmitter()
  assert.Nil(t, err);
	ctx := context.Background()
  a.repo, err = NewTestOrdersDB(t, ctx, em)
  a.initEventEmitter()
  assert.Nil(t, err);
  assert.NotNil(t, a.repo);
	a.ordersAPI = NewOrdersAPI(a.repo)
	a.gEngine = gin.Default()
	a.gEngine.Use(
		middleware.Logging(),
		middleware.EventsBuilder(),
  )
  a.gEngine.Use(cors.Default())
	a.initRoutes()
  return a, ctx
}

func CloseTestApp(a *App) {
	a.ordersAPI.repo.Close()
}

// NewTestOrdersDB is a helper that returns an open connection to a unique and isolated
// test database, fully migrated and ready for testing, it will be deleted if the
// tests succeed and will NOT be deleted if tests fail.
func NewTestOrdersDB(t *testing.T, ctx context.Context, em events.EventEmitter) (*repo.OrdersDB, error) {
	common.LoadConfig()
  gm := golangmigrator.New("../db/migrations")
	config := pgtestdb.Config{
    DriverName: "postgres",
    User:       common.Config.PgUser,
    Password:   common.Config.PgPass,
    Host:       common.Config.PgHost,
    Port:       common.Config.PgPort,
		Database:   url.QueryEscape(common.Config.PgDbName),
    Options:    "sslmode=disable",
  }
	if err := gm.Migrate(ctx, nil, config); err != nil {
		if err == migrate.ErrNoChange {
			fmt.Printf("Migrations ok, no change.\n")
		} else {
			return nil, err
		}
	}
  testDb := pgtestdb.Custom(t, config, gm)
  assert.NotEqual(t, nil, testDb)
	fmt.Printf("Test db URL: %s", testDb.URL())
	return repo.NewOrdersDBUrl(ctx, testDb.URL(), em)
}

func NewRequestAsUser(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
  ctx := r.Context()
  claims := &middleware.IDTokenClaims{
		Sub: USER_KEY,
	  RealmAccess: middleware.Roles{ Roles: []string{"something"} },
	}
  ctx = context.WithValue(ctx, common.CtxAuthClaims, claims)
  return r.WithContext(ctx)
}

func POST(t *testing.T, a *App, path string, request interface{}, expectedCode int) gin.H {
	accountReqJsonValue, _ := json.Marshal(request)
	w := httptest.NewRecorder()
	a.gEngine.ServeHTTP(w, NewRequestAsUser("POST", path, bytes.NewBuffer(accountReqJsonValue)))
	assert.Equal(t, expectedCode, w.Code)

  var got gin.H
	outputBytes := w.Body.Bytes()
	// fmt.Printf("\nOutput: %+v\n", outputBytes)
  err := json.Unmarshal(outputBytes, &got)
  if err != nil {
    t.Fatal(err)
  }
  return got
}

func Test_duplicate_card_key(t *testing.T) {
  a, _ := NewTestApp(t)
  defer CloseTestApp(a)

	accountReq := repo.Account{UserKey: null.StringFrom(USER_KEY)}
  got := POST(t, a, "/v2/account/", accountReq, http.StatusCreated)
  fmt.Printf("Added account: %+v\n" ,got)
  accountID := int(got["data"].(float64))

  orderReq := repo.Order{Amount: null.Float64From(15), AccountID: null.IntFrom(accountID)}
  got = POST(t, a, "/v2/order/", orderReq, http.StatusCreated)
  fmt.Printf("Added order: %+v\n" ,got)
  orderID := int(got["data"].(float64))

	data := repo.RequestUpdateToken{
		CardExp: "0627",
		CardNumber: "1234123412341234",
		OrderId: orderID,
		ParamX: fmt.Sprintf("new_token_%d", orderID),
    Token: "1234Token",
	}
	fmt.Printf("Data: %+v\n", data)
  got = POST(t, a, "/v2/order/update_token", data, http.StatusOK)
  fmt.Printf("Update token: %+v\n" ,got)
  assert.Equal(t, true, got["success"].(bool))

  // Secod update token with same card.
	data = repo.RequestUpdateToken{
		CardExp: "0627",
		CardNumber: "1234123412341234",
		OrderId: orderID,
		ParamX: fmt.Sprintf("new_token_%d", orderID),
    Token: "4321Token",
	}
	fmt.Printf("Data: %+v\n", data)
  got = POST(t, a, "/v2/order/update_token", data, http.StatusOK)
  fmt.Printf("Update token: %+v\n" ,got)
  assert.Equal(t, true, got["success"].(bool))
}
