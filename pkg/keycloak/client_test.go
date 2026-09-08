package keycloak

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nerzal/gocloak/v13"
	"github.com/golang-jwt/jwt/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Token() is the reader that dereferences what Invalidate nils, so it is the
// side worth pinning: exercising only the clearers stayed green with
// AccessToken's lock deleted.
//
// No Keycloak needed — the assertion is the race detector, not the return value.
func TestClientTokenReadRacesInvalidation(t *testing.T) {
	client := &Client{kc: gocloak.NewClient("http://127.0.0.1:1")}
	// NewClient is bypassed, so its timeout is not in place: a port that drops
	// rather than refuses would hang the test with the mutex held.
	client.kc.RestyClient().SetTimeout(time.Second)

	seedToken(client, "cached")

	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 150; j++ {
				_, _ = client.Token()
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 150; j++ {
				if worker%2 == 0 {
					client.InvalidateToken("cached")
				} else {
					client.Invalidate()
				}
				seedToken(client, "cached")
			}
		}(i)
	}

	wg.Wait()
}

// seedToken puts a valid cached token in place, which no exported method can do
// without a Keycloak to log in against.
func seedToken(c *Client, access string) {
	claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.token = &gocloak.JWT{AccessToken: access}
	c.claims = &claims
}

// The renewal run shares one Client across maxWorkers goroutines. Run with
// -race, this fails if the mutex is removed from either clearer. Login is not
// exercised; the part that broke is a cached token read while another goroutine
// clears it.
func TestClientConcurrentTokenAccess(t *testing.T) {
	claims := jwt.MapClaims{}
	client := &Client{
		token:  &gocloak.JWT{AccessToken: "cached"},
		claims: &claims,
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				client.InvalidateToken("cached")
				client.Invalidate()

				client.mu.Lock()
				client.token = &gocloak.JWT{AccessToken: "cached"}
				client.claims = &claims
				client.mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

// A worker reacting to a 401 must not discard a token another worker already
// replaced, or one expiry costs a login per worker.
func TestInvalidateTokenClearsOnlyTheNamedToken(t *testing.T) {
	claims := jwt.MapClaims{}
	client := &Client{
		token:  &gocloak.JWT{AccessToken: "fresh"},
		claims: &claims,
	}

	client.InvalidateToken("stale")

	if client.token == nil {
		t.Fatal("a token that was not the rejected one must survive")
	}
	if client.claims == nil {
		t.Fatal("claims must survive with their token")
	}

	client.InvalidateToken("fresh")

	if client.token != nil || client.claims != nil {
		t.Fatal("the rejected token must be cleared")
	}
}

func TestInvalidateTokenOnEmptyCacheIsHarmless(t *testing.T) {
	client := &Client{}

	client.InvalidateToken("anything")

	if client.token != nil || client.claims != nil {
		t.Fatal("nothing was cached, so nothing should appear")
	}
}

// Nothing is cached on failure, so without a backoff every caller pays the full
// request timeout with the mutex held — hours, serially, for a run of a few
// thousand orders.
func TestAccessTokenSkipsOnlyAfterConsecutiveFailures(t *testing.T) {
	// Written by the handler goroutine, read here: no happens-before edge comes
	// from the HTTP round trip itself.
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{kc: gocloak.NewClient(server.URL)}
	client.kc.RestyClient().SetTimeout(time.Second)

	// Up to the threshold every caller still gets its own attempt: a blip that
	// heals must not cost a whole window of orders.
	for i := 0; i < loginFailureThreshold; i++ {
		if _, err := client.Token(); err == nil {
			t.Fatal("a 500 from Keycloak should not yield a token")
		}
	}
	if got := requests.Load(); got != int64(loginFailureThreshold) {
		t.Fatalf("expected %d attempts before suppression, got %d", loginFailureThreshold, got)
	}

	// Past it, callers fail without adding load.
	for i := 0; i < 5; i++ {
		if _, err := client.Token(); err == nil {
			t.Fatal("expected the backoff to keep failing callers fast")
		}
	}
	if got := requests.Load(); got != int64(loginFailureThreshold) {
		t.Fatalf("backoff should have suppressed further attempts, got %d", got)
	}
}

// The threshold is a literal on purpose: every other assertion here derives its
// bounds from the constant, which restates it instead of constraining it — a
// threshold of 1 or of 100 keeps those green.
//
// The window's lower bound is the exact complement of the compile-time guard in
// client.go, so it can only fire if that guard is deleted. The upper bound is a
// literal and is the one that can fire otherwise.
func TestBackoffConstantsHaveTheirIntendedValues(t *testing.T) {
	if loginFailureThreshold != 3 {
		t.Errorf("threshold is %d: 1 suppresses on a single blip, and a suppressed "+
			"charge worker still writes a pending payment row per order; a large value "+
			"never suppresses and restores the serial grind", loginFailureThreshold)
	}

	if loginFailureBackoff <= 4*tokenRequestTimeout || loginFailureBackoff > 2*time.Minute {
		t.Errorf("backoff window is %v: it has to outlast the worst case of one attempt "+
			"(four requests at %v) without freezing callers for minutes",
			loginFailureBackoff, tokenRequestTimeout)
	}
}

// The count that matters is the one before suppression: two consecutive
// failures must each still reach Keycloak, and the third is the one that closes
// the door. Written as literals so the constant cannot drift underneath it.
func TestSecondFailureStillTriesAndThirdSuppresses(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{kc: gocloak.NewClient(server.URL)}
	client.kc.RestyClient().SetTimeout(time.Second)

	_, _ = client.Token()
	if got := requests.Load(); got != 1 {
		t.Fatalf("first call: %d attempts, want 1", got)
	}

	_, _ = client.Token()
	if got := requests.Load(); got != 2 {
		t.Fatalf("second call must still reach Keycloak: %d attempts, want 2", got)
	}

	_, _ = client.Token()
	if got := requests.Load(); got != 3 {
		t.Fatalf("third call must still reach Keycloak: %d attempts, want 3", got)
	}

	_, _ = client.Token()
	if got := requests.Load(); got != 3 {
		t.Fatalf("fourth call must be suppressed: %d attempts, want 3", got)
	}
}

// One or two failures with a success between them must suppress nothing: a
// suppressed charge worker still writes a pending payment row per order, so a
// window of fast failures books orders failed that a healing blip would not.
func TestFailuresBelowThresholdDoNotSuppress(t *testing.T) {
	client := &Client{}

	for i := 0; i < loginFailureThreshold-1; i++ {
		client.recordFailure()
	}
	if client.backingOff() {
		t.Fatalf("%d consecutive failures must not suppress anything", loginFailureThreshold-1)
	}

	client.recordFailure()
	if !client.backingOff() {
		t.Fatal("the threshold should be reached now")
	}

	// A success anywhere in the sequence resets the count.
	client.clearFailures()
	for i := 0; i < loginFailureThreshold-1; i++ {
		client.recordFailure()
	}
	if client.backingOff() {
		t.Fatal("a success in between must reset the consecutive count")
	}
}

// A caller holding a valid token must never be refused because obtaining a new
// one has been failing: the backoff concerns fetching, not using.
func TestValidCachedTokenIsReturnedWhileBackingOff(t *testing.T) {
	client := &Client{kc: gocloak.NewClient("http://127.0.0.1:1")}
	client.kc.RestyClient().SetTimeout(time.Second)
	seedToken(client, "still-good")

	client.mu.Lock()
	client.failures = loginFailureThreshold
	client.lastFailure = time.Now()
	client.mu.Unlock()

	token, err := client.Token()
	if err != nil {
		t.Fatalf("a valid cached token should be returned during a backoff: %v", err)
	}
	if token != "still-good" {
		t.Fatalf("got %q", token)
	}
}

// An invalidation says the caller knows the token is bad, which has to outrank
// the backoff — otherwise the retry after a 401 is suppressed and becomes no
// retry at all.
func TestInvalidationClearsTheBackoff(t *testing.T) {
	client := &Client{}
	for i := 0; i < loginFailureThreshold; i++ {
		client.recordFailure()
	}

	client.Invalidate()
	if client.backingOff() {
		t.Fatal("Invalidate must clear the backoff")
	}

	seedToken(client, "cached")
	for i := 0; i < loginFailureThreshold; i++ {
		client.recordFailure()
	}

	client.InvalidateToken("cached")
	if client.backingOff() {
		t.Fatal("InvalidateToken must clear the backoff when it clears the token")
	}
}

// NewClient is what production uses, and the timeout it sets is what bounds a
// hung Keycloak. Deleting that line used to leave the whole suite green.
func TestNewClientBoundsItsRequests(t *testing.T) {
	saved := common.Config.KeycloakServerUrl
	common.Config.KeycloakServerUrl = "http://127.0.0.1:1"
	defer func() { common.Config.KeycloakServerUrl = saved }()

	client := NewClient()

	if got := client.kc.RestyClient().GetClient().Timeout; got != tokenRequestTimeout {
		t.Fatalf("NewClient must bound its requests: timeout is %v, want %v", got, tokenRequestTimeout)
	}
}

// A backoff that outlived its window would keep failing callers after Keycloak
// came back.
func TestBackingOffExpires(t *testing.T) {
	client := &Client{
		failures:    loginFailureThreshold,
		lastFailure: time.Now().Add(-loginFailureBackoff - time.Second),
	}

	if client.backingOff() {
		t.Fatal("failures older than the window must not suppress anything")
	}

	client.lastFailure = time.Now()
	if !client.backingOff() {
		t.Fatal("failures at the threshold, inside the window, must suppress")
	}
}
