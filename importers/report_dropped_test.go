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

// The permanent nuisance and a real format break have to be separate Sentry
// issues, or archiving the first swallows the second.
//
// 10 of 200 is the case the ratio bucketing got wrong: at a 10% boundary it
// shared a bucket with the permanent single bad row, so ten people silently
// losing their specials was archived along with a totals line.
func TestReportDroppedRows_ASmallBreakIsNotTheSingleRowNuisance(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 1, 199)  // the row nobody will fix
	reportDroppedRows(NewSpecialsImporter(), 10, 190) // ten rows suddenly unparseable

	require.Len(t, transport.events, 2)
	assert.NotEqual(t, transport.events[0].Fingerprint, transport.events[1].Fingerprint,
		"archiving the permanent 1-of-200 must not archive a 10-row break")
}

func TestReportDroppedRows_ALargeBreakIsItsOwnIssue(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 1, 199)
	reportDroppedRows(NewSpecialsImporter(), 30, 170)

	require.Len(t, transport.events, 2)
	assert.NotEqual(t, transport.events[0].Fingerprint, transport.events[1].Fingerprint)
}

// …while the same nuisance keeps one issue however the sheet grows around it.
// The bucket is the absolute count for exactly this reason: on a ratio, one bad
// row of 10 and one bad row of 11 were different buckets.
func TestReportDroppedRows_TheSameNuisanceGroupsAsTheSheetGrows(t *testing.T) {
	transport := withCapturedSentry(t)

	for _, kept := range []int{9, 10, 199, 250, 2000} {
		reportDroppedRows(NewSpecialsImporter(), 1, kept)
	}

	require.Len(t, transport.events, 5)
	for i := 1; i < len(transport.events); i++ {
		assert.Equal(t, transport.events[0].Fingerprint, transport.events[i].Fingerprint,
			"one unfixable row must stay one issue as the sheet grows")
	}
}

// Nothing survived stays its own issue at its own level.
func TestReportDroppedRows_TheErrorLevelIsItsOwnIssue(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 1, 40)
	reportDroppedRows(NewSpecialsImporter(), 1, 0)

	require.Len(t, transport.events, 2)
	assert.NotEqual(t, transport.events[0].Fingerprint, transport.events[1].Fingerprint)
	assert.Equal(t, "error", string(transport.events[1].Level))
}
