package importers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// withFailingDB makes Init fail at the repo step: CreateEmitter still succeeds
// with an empty NatsUrl, and nothing listens on port 1.
func withFailingDB(t *testing.T) {
	t.Helper()

	saved := common.Config
	t.Cleanup(func() { common.Config = saved })

	common.Config.NatsUrl = ""
	common.Config.PgHost, common.Config.PgPort = "127.0.0.1", "1"
}

// Close runs on the Init failure path, so it has to survive a partial Init.
// Driving Init for real is the point: calling Close on a zero-value struct, as
// this test first did, passes whether or not the bug is present.
func TestBaseImporterCloseAfterFailedInit(t *testing.T) {
	withFailingDB(t)

	im := NewBaseImporter()
	require.Error(t, im.Init(), "the repo step must fail for this test to mean anything")
	require.Nil(t, im.repo, "a failed Init must leave no typed nil behind")

	im.Close()
}
