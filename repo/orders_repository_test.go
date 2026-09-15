package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/events"
)

// Guards the one behavioural difference in the pgx v4 → v5 migration.
//
// v4's pgxpool.Connect dialled before returning. v5's pgxpool.New only parses
// the DSN and hands ctx to a background goroutine that creates minConns idle
// resources — zero by default — so nothing connects and the constructor
// succeeds against a dead database. NewOrdersDBUrl calls Ping to restore the old
// contract.
//
// Without this test, dropping the Ping passes every other test in the tree:
// nothing else here constructs the pool against anything but a live database,
// and the caller-side error handling this protects lives in cmd, api and
// importers, which have no coverage of an unreachable one. The regression would
// surface as Init reporting success and the first query failing mid-workflow.
//
// Port 1 is reserved and refuses immediately; connect_timeout bounds the case
// where something is listening on a machine that runs this.
func TestNewOrdersDBUrl_FailsOnUnreachableDB(t *testing.T) {
	db, err := NewOrdersDBUrl(context.Background(),
		"postgres://x@127.0.0.1:1/db?sslmode=disable&connect_timeout=2",
		new(events.NoopEmitter))

	require.Error(t, err, "constructor accepted an unreachable database: pgxpool.New is lazy, so Ping is what makes this fail")
	require.Nil(t, db, "a failed construction must not hand back a usable pool")
}
