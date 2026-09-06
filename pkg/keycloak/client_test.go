package keycloak

import (
	"sync"
	"testing"

	"github.com/Nerzal/gocloak/v13"
	"github.com/golang-jwt/jwt/v4"
)

// The renewal run charges through maxWorkers goroutines sharing one Client, so
// these fields are read and written concurrently. Run with -race, this fails if
// the mutex is removed.
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
