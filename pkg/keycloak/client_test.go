package keycloak

import (
	"sync"
	"testing"
	"time"

	"github.com/Nerzal/gocloak/v13"
	"github.com/golang-jwt/jwt/v4"
)

// Token() is the reader every charge goes through, and the one that dereferences
// what Invalidate nils, so it is the side worth pinning: an earlier version of
// this file exercised only the clearers and stayed green with AccessToken's lock
// deleted.
//
// No Keycloak is needed. The seeded claims carry a future exp, so a cache hit
// returns without a network call; once a clearer wins, the login attempt goes to
// a closed port and fails immediately, which is fine — the assertion is the race
// detector, not the return value.
func TestClientTokenReadRacesInvalidation(t *testing.T) {
	client := &Client{kc: gocloak.NewClient("http://127.0.0.1:1")}
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

// The renewal run charges through maxWorkers goroutines sharing one Client, so
// these fields are read and written concurrently. Run with -race, this fails if
// the mutex is removed from either clearer.
//
// Login is not exercised: it needs a Keycloak. What is exercised is the part
// that broke — a cached token being read while another goroutine clears it.
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

// A worker reacting to a 401 must not throw away a token another worker already
// replaced: otherwise one expiry costs a login per worker, and a credential
// that is rejected for any other reason costs one per charge in the run.
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
