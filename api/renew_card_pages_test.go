package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pageOpener(calls *atomic.Int32, body string, shareable bool, err error) func(context.Context) ([]byte, bool, error) {
	return func(context.Context) ([]byte, bool, error) {
		calls.Add(1)
		return []byte(body), shareable, err
	}
}

func TestRenewCardPages_ConcurrentRequestsShareOnePage(t *testing.T) {
	pages := newRenewCardPages()
	release := make(chan struct{})
	var calls atomic.Int32
	slowOpen := func(context.Context) ([]byte, bool, error) {
		calls.Add(1)
		<-release
		return []byte(`{"url":"p1"}`), true, nil
	}

	const clicks = 23
	var wg sync.WaitGroup
	bodies := make([]string, clicks)
	var reusedCount atomic.Int32
	for i := range clicks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, reused, err := pages.open(context.Background(), "new_token_1", slowOpen)
			assert.NoError(t, err)
			bodies[i] = string(body)
			if reused {
				reusedCount.Add(1)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, int32(clicks-1), reusedCount.Load())
	for _, body := range bodies {
		assert.Equal(t, `{"url":"p1"}`, body)
	}
}

func TestRenewCardPages_ReusedWithinTheWindowThenOpenedAgain(t *testing.T) {
	pages := newRenewCardPages()
	now := time.Date(2026, 11, 15, 8, 1, 28, 0, time.UTC)
	pages.now = func() time.Time { return now }
	var calls atomic.Int32

	_, reused, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	assert.False(t, reused)

	now = now.Add(renewCardPageReuse - time.Second)
	body, reused, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)
	assert.True(t, reused)
	assert.Equal(t, `{"url":"p1"}`, string(body))

	now = now.Add(time.Second)
	body, reused, err = pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)
	assert.False(t, reused)
	assert.Equal(t, `{"url":"p2"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

func TestRenewCardPages_OtherOrdersGetTheirOwnPage(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	_, _, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	body, reused, err := pages.open(context.Background(), "new_token_2", pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, `{"url":"p2"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

// A failure is not kept: the next click must try again, not get the failure.
func TestRenewCardPages_FailureIsNotKept(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	_, _, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, "", false, errors.New("checkout down")))
	require.Error(t, err)
	body, reused, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, `{"url":"p1"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

// An answer that is not a page (checkout's error body, a proxy's HTML) is
// returned to its own caller but not handed to the next one.
func TestRenewCardPages_UnshareableAnswerIsNotKept(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	_, _, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, "<html>502</html>", false, nil))
	require.NoError(t, err)
	_, reused, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, int32(2), calls.Load())
}

func TestRenewCardPages_ForgetDropsThePage(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	_, _, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	pages.forget("new_token_1")
	_, reused, err := pages.open(context.Background(), "new_token_1", pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, int32(2), calls.Load())
}

// Without a reference every request falls back to the same one, so the member
// tells them apart.
func TestRenewCardPageKey_FallbackReferenceIsPerMember(t *testing.T) {
	assert.Equal(t, "new_token_7", renewCardPageKey("new_token_7", "kc-a"))
	assert.NotEqual(t,
		renewCardPageKey(defaultRenewCardReference, "kc-a"),
		renewCardPageKey(defaultRenewCardReference, "kc-b"))
}
