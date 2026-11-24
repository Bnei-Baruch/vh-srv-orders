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

// A partial drop must still reach Sentry, or the dropped people silently get nothing.
func TestReportDroppedRows_PartialDropReachesSentry(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 30, 170)
	require.True(t, sentry.Flush(time.Second))

	require.Len(t, transport.events, 1)
	assert.Contains(t, transport.events[0].Message, "dropped 30 of 200")
	assert.Contains(t, transport.events[0].Message, "importer specials")
	assert.Equal(t, sentry.LevelWarning, transport.events[0].Level, "some rows landed")
}

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

// The permanent single bad row and a real break must be separate Sentry
// issues, or archiving the first swallows the second.
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

// The same nuisance keeps one issue however the sheet grows around it.
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

func TestReportDroppedRows_TheErrorLevelIsItsOwnIssue(t *testing.T) {
	transport := withCapturedSentry(t)

	reportDroppedRows(NewSpecialsImporter(), 1, 40)
	reportDroppedRows(NewSpecialsImporter(), 1, 0)

	require.Len(t, transport.events, 2)
	assert.NotEqual(t, transport.events[0].Fingerprint, transport.events[1].Fingerprint)
	assert.Equal(t, "error", string(transport.events[1].Level))
}
