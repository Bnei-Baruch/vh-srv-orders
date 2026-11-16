package pelecard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

type staticTokens string

func (s staticTokens) Token() (string, error) { return string(s), nil }
func (staticTokens) Invalidate()              {}
func (staticTokens) InvalidateToken(string)   {}

// A stalled read gives up at paymentReadTimeout, not at the client's
// charge-sized timeout.
func TestGetPayment_GivesUpAtItsOwnTimeout(t *testing.T) {
	saved := paymentReadTimeout
	paymentReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { paymentReadTimeout = saved })

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	c := &Client{Client: resty.New().SetTimeout(time.Minute), BaseURL: srv.URL, Tokens: staticTokens("tok")}
	start := time.Now()
	_, err := c.GetPayment(context.Background(), "ord-5", "m-7")

	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %s, want about %s", d, paymentReadTimeout)
	}
}
