package repo

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
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

// The pool is held in a field, not embedded, so nothing outside this package
// can reach the database except through a method declared here — which is
// where the events are emitted. A raw write past that layer lands with no
// event and nothing downstream hears about it.
//
// That used to be enforced by api/repo_surface_test.go, 494 lines of AST
// analysis over the api package, because while *OrdersDB embedded
// *pgxpool.Pool the methods were promoted onto every holder of the concrete
// type. Embedding it again would restore the promotion silently: no call site
// changes, nothing fails to build, and the barrier is simply gone. This is
// what notices.
//
// Exec stands for the whole promoted set; they arrive and leave together.
func TestOrdersDB_DoesNotExposeThePool(t *testing.T) {
	var v any = &OrdersDB{}
	_, ok := v.(interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	})
	require.False(t, ok,
		"*OrdersDB satisfies an Exec-shaped interface, so the pool is embedded again — "+
			"every holder of the concrete type can now write SQL past the repo layer and skip its events")
}
