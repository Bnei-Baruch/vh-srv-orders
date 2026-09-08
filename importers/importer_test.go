package importers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// withFailingDB points the config at a port nothing listens on, and empties
// NatsUrl so CreateEmitter still succeeds — that ordering is what makes Init
// fail at the repo step, which is the step that matters here.
func withFailingDB(t *testing.T) {
	t.Helper()

	saved := common.Config
	t.Cleanup(func() { common.Config = saved })

	common.Config.NatsUrl = ""
	common.Config.PgHost, common.Config.PgPort = "127.0.0.1", "1"
}

// doImport calls Close on the Init failure path, so Close has to survive a
// partial Init. The case that bites is the repo: NewOrdersDB returns a concrete
// *repo.OrdersDB, so assigning it before checking the error boxes a nil pointer
// into a non-nil interface — Close's nil guard passes and the call panics on the
// nil receiver, before the emitter is ever drained.
//
// Driving Init for real is the point. Calling Close on a zero-value struct, as
// this test first did, passes either way and proves nothing.
func TestBaseImporterCloseAfterFailedInit(t *testing.T) {
	withFailingDB(t)

	im := NewBaseImporter()
	require.Error(t, im.Init(), "the repo step must fail for this test to mean anything")
	require.Nil(t, im.repo, "a failed Init must leave no typed nil behind")

	im.Close()
}
