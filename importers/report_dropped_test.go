package importers

import (
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureTransport records the events a test's Sentry client would have sent.
type captureTransport struct {
	events []*sentry.Event
}

func (t *captureTransport) Configure(sentry.ClientOptions) {}
func (t *captureTransport) SendEvent(e *sentry.Event)      { t.events = append(t.events, e) }
func (t *captureTransport) Flush(time.Duration) bool       { return true }

func withCapturedSentry(t *testing.T) *captureTransport {
	t.Helper()
	transport := &captureTransport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{Transport: transport}))
	return transport
}

// A partial format break used to page and now does not: before the parsers
// learned to skip, one unparseable date returned an error and doImport turned
// that into CaptureException plus a non-zero exit. 30 rows dropped out of 200
// must still reach Sentry, or those 30 people silently never get their
// specials and the only trace is a log line nobody reads.
func TestReportDroppedRows_PartialDropReachesSentry(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 30, 170)
	require.True(t, sentry.Flush(time.Second))

	require.Len(t, transport.events, 1)
	assert.Contains(t, transport.events[0].Message, "dropped 30 of 200")
	assert.Contains(t, transport.events[0].Message, "importer specials")
	assert.Equal(t, sentry.LevelWarning, transport.events[0].Level, "some rows landed")
}

// Nothing surviving used to abort the import, which doImport turns into
// os.Exit(1) — and since the sheet is not cleared between runs, the cron then
// exited 1 on every invocation until a human edited it. Two operators typing
// GBP satisfy the same condition as a changed sheet format, so the distinction
// is worth an alert level, not a dead importer.
func TestReportDroppedRows_NothingSurvivedIsAnErrorLevelEvent(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 2, 0)
	require.True(t, sentry.Flush(time.Second))

	require.Len(t, transport.events, 1)
	assert.Equal(t, sentry.LevelError, transport.events[0].Level)
	assert.Contains(t, transport.events[0].Message, "every one of 2 sheet rows")
}

func TestReportDroppedRows_CleanRunIsSilent(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 0, 200)
	require.True(t, sentry.Flush(time.Second))

	assert.Empty(t, transport.events, "a run that dropped nothing has nothing to report")
}
