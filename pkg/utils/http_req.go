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

// outboundTimeout is the only bound on these calls, not a backstop for one.
//
// Every caller is a payment handler passing a context with cancellation
// stripped, so nothing shortens the call: not a client disconnect, which is the
// point, and not the shutdown either.
//
// Which means it outlasts the shutdown deliberately, and a POST that starts
// just before a signal can be cut off by the process exiting rather than by
// anything here — api.shutdownGrace is 15s and the rest of the exit is another
// 13s, against these 30s. Nothing corrupts today because the three handlers do
// no database work after the POST; the next one that does would meet a closed
// pool. Making a payment call survive the exit means the shutdown waiting for
// in-flight ones, which is request tracking rather than a deadline.
const outboundTimeout = 30 * time.Second
