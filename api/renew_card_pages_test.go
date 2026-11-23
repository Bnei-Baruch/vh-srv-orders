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

// clickPayload is what one click on "Update card" sends; repeated clicks send
// the same bytes.
var clickPayload = []byte(`{"Reference":"new_token_1","GoodURL":"https://bo.example/customers/7?user_email=a@example.com&order_id=1"}`)

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
		wg.Go(func() {
			body, reused, err := pages.open(context.Background(), "new_token_1", clickPayload, slowOpen)
			assert.NoError(t, err)
			bodies[i] = string(body)
			if reused {
				reusedCount.Add(1)
			}
		})
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

	_, reused, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	assert.False(t, reused)

	now = now.Add(renewCardPageReuse - time.Second)
	body, reused, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)
	assert.True(t, reused)
	assert.Equal(t, `{"url":"p1"}`, string(body))

	now = now.Add(time.Second)
	body, reused, err = pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)
	assert.False(t, reused)
	assert.Equal(t, `{"url":"p2"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

func TestRenewCardPages_OtherOrdersGetTheirOwnPage(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	_, _, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	body, reused, err := pages.open(context.Background(), "new_token_2", clickPayload, pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, `{"url":"p2"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

// A failure is not kept: the next click must try again, not get the failure.
func TestRenewCardPages_FailureIsNotKept(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	_, _, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, "", false, errors.New("checkout down")))
	require.Error(t, err)
	body, reused, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p1"}`, true, nil))
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

	_, _, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, "<html>502</html>", false, nil))
	require.NoError(t, err)
	_, reused, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, int32(2), calls.Load())
}

// Back from a cancelled or declined page, the back office builds its return
// URLs from an address that carries status=failed, so the next request differs
// and must not get the page Pelecard has closed.
func TestRenewCardPages_DifferentRequestForTheSameOrderGetsANewPage(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32
	afterCancel := []byte(`{"Reference":"new_token_1","GoodURL":"https://bo.example/customers/7?status=failed&user_email=a@example.com?user_email=a@example.com&order_id=1"}`)

	_, _, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	body, reused, err := pages.open(context.Background(), "new_token_1", afterCancel, pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, `{"url":"p2"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

func TestRenewCardPages_ForgetDropsThePage(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32

	other := []byte(`{"Reference":"new_token_1","GoodURL":"https://bo.example/other"}`)
	_, _, err := pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p1"}`, true, nil))
	require.NoError(t, err)
	_, _, err = pages.open(context.Background(), "new_token_1", other, pageOpener(&calls, `{"url":"p1b"}`, true, nil))
	require.NoError(t, err)
	pages.forget("new_token_1")
	_, reused, err := pages.open(context.Background(), "new_token_1", other, pageOpener(&calls, `{"url":"p2b"}`, true, nil))
	require.NoError(t, err)
	assert.False(t, reused, "every page kept for the order is dropped")
	_, reused, err = pages.open(context.Background(), "new_token_1", clickPayload, pageOpener(&calls, `{"url":"p2"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, int32(4), calls.Load())
}

// Two members renewing without a reference share the fallback one, and must
// not get each other's page: their payloads differ.
func TestRenewCardPages_FallbackReferenceIsNotSharedAcrossMembers(t *testing.T) {
	pages := newRenewCardPages()
	var calls atomic.Int32
	memberA := []byte(`{"Reference":"new_token_by_admin_no_cvv","Name":"A","Email":"a@example.com"}`)
	memberB := []byte(`{"Reference":"new_token_by_admin_no_cvv","Name":"B","Email":"b@example.com"}`)

	_, _, err := pages.open(context.Background(), "new_token_by_admin_no_cvv", memberA, pageOpener(&calls, `{"url":"pa"}`, true, nil))
	require.NoError(t, err)
	body, reused, err := pages.open(context.Background(), "new_token_by_admin_no_cvv", memberB, pageOpener(&calls, `{"url":"pb"}`, true, nil))
	require.NoError(t, err)

	assert.False(t, reused)
	assert.Equal(t, `{"url":"pb"}`, string(body))
	assert.Equal(t, int32(2), calls.Load())
}

// Clicks waiting on a call that fails get the failure as reused, so the
// handler reports it once rather than once per click.
func TestRenewCardPages_WaitersOnAFailureAreReused(t *testing.T) {
	pages := newRenewCardPages()
	release := make(chan struct{})
	failing := func(context.Context) ([]byte, bool, error) {
		<-release
		return nil, false, errors.New("checkout down")
	}

	const clicks = 5
	var wg sync.WaitGroup
	var reportable atomic.Int32
	for range clicks {
		wg.Go(func() {
			_, reused, err := pages.open(context.Background(), "new_token_1", clickPayload, failing)
			assert.Error(t, err)
			if !reused {
				reportable.Add(1)
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.Equal(t, int32(1), reportable.Load())
}
