package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Same as importers.TestBaseImporterCloseAfterFailedInit: Do calls Close on the
// Init failure path, and a repo assigned before its error is checked leaves a
// typed nil that defeats Close's guard.
func TestWorkerCloseAfterFailedInit(t *testing.T) {
	saved := common.Config
	t.Cleanup(func() { common.Config = saved })
	common.Config.NatsUrl = ""
	common.Config.PgHost, common.Config.PgPort = "127.0.0.1", "1"

	w := NewWorker()
	require.Error(t, w.Init(), "the repo step must fail for this test to mean anything")
	require.Nil(t, w.repo, "a failed Init must leave no typed nil behind")

	w.Close()
}
