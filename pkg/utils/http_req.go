package utils

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// PostJSON posts payload and returns the response.
//
// The timeout is what bounds this, not the context: the payment handlers pass a
// context stripped of cancellation on purpose, because a caller going away must
// not abandon a call that may already have charged. So the deadline has to hold
// even when nothing can be cancelled.
//
// The context is still worth taking for the request's values. Cancelling
// through it is not something any caller does today: all three are payment
// handlers, and all three pass a context with cancellation stripped.
func PostJSON(ctx context.Context, method string, url string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("http.NewRequestWithContext: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: outboundTimeout}
	return client.Do(req)
}

// outboundTimeout bounds one outbound call. Longer than any shutdown grace on
// purpose: cancellation is what shortens these during a shutdown, and this is
// only the backstop for a peer that accepts a connection and never answers.
const outboundTimeout = 30 * time.Second
