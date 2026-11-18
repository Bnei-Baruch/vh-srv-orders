package api

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// renewCardPageReuse is how long a card-renewal page, once opened, is handed
// to further requests for the same order instead of opening another.
const renewCardPageReuse = 2 * time.Minute

// renewCardPages opens at most one card-renewal page per order at a time.
// The back office's "Update card" button once sent 23 requests for one order
// in 9 seconds, and each opened its own Pelecard page: requests that arrive
// while one is in flight wait for it and share its answer, and requests within
// renewCardPageReuse of it get the same page. Only a successful answer is
// kept. Per process: orders runs as a single instance.
type renewCardPages struct {
	inFlight singleflight.Group

	mu     sync.Mutex
	recent map[string]renewCardPage
	now    func() time.Time
}

type renewCardPage struct {
	body     []byte
	openedAt time.Time
}

func newRenewCardPages() *renewCardPages {
	return &renewCardPages{recent: map[string]renewCardPage{}, now: time.Now}
}

// open returns the page for key, calling openPage only if no page for key is
// in flight or recent. reused reports that the answer came from another
// request. openPage reports whether its answer may be shared.
func (r *renewCardPages) open(ctx context.Context, key string,
	openPage func(context.Context) (body []byte, shareable bool, err error)) (body []byte, reused bool, err error) {

	if body, ok := r.lookup(key); ok {
		return body, true, nil
	}

	// Do reports shared for the caller that ran the call too, once anyone else
	// waited on it, so whether this caller opened the page is tracked here.
	opened := false
	value, err, _ := r.inFlight.Do(key, func() (interface{}, error) {
		opened = true
		// Not the first caller's context: if that browser goes away, the
		// requests waiting on this call still want the page.
		body, shareable, err := openPage(context.WithoutCancel(ctx))
		if err == nil && shareable {
			r.remember(key, body)
		}
		return body, err
	})
	if err != nil {
		return nil, !opened, err
	}
	return value.([]byte), !opened, nil
}

func (r *renewCardPages) lookup(key string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	page, ok := r.recent[key]
	if !ok || r.now().Sub(page.openedAt) >= renewCardPageReuse {
		return nil, false
	}
	return page.body, true
}

func (r *renewCardPages) remember(key string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	for k, page := range r.recent {
		if now.Sub(page.openedAt) >= renewCardPageReuse {
			delete(r.recent, k)
		}
	}
	r.recent[key] = renewCardPage{body: body, openedAt: now}
}

// forget drops the page kept for key, once the card it was opened for has been
// saved: a page Pelecard has already completed must not be handed out again.
func (r *renewCardPages) forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.recent, key)
}
