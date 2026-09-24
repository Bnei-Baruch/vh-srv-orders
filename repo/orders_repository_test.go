package repo

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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

// The invariant both tests below hold: nothing outside this package can reach
// the database except through a method declared here, which is where the
// events are emitted. A raw write past that layer lands with no event and
// nothing downstream hears about it.
//
// It used to be held by api/repo_surface_test.go — 494 lines of AST analysis
// over the api package, matching call shapes. That was the wrong side to
// stand on: it enumerated the ways api could spell a bypass, so every new
// spelling was a hole until someone added it. These two stand on the repo
// side, where the surface is finite and enumerable:
//
//   - the unexported half is the compiler's. pool is lowercase, so no code
//     outside repo can name it, whatever it spells.
//   - the exported half is these tests'. An exported field or a method
//     handing back a live handle compiles fine and puts the bypass straight
//     back, and the compiler has no opinion about it.
//
// Still not covered, by either: GetDBURL is exported, so a determined caller
// can open its own pool. That is a separate hole and predates all of this.

// Embedding *pgxpool.Pool again would restore the promoted Exec/Query/Begin
// silently — no call site changes, nothing fails to build, and the barrier is
// gone. This is what notices.
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

// Walks the exported surface of *OrdersDB and fails on anything that hands a
// caller something it can write SQL through: an exported field of a handle
// type, a method returning one, or either of those carrying one inside.
//
// The named-field refactor removed the promotion, not the possibility. Each of
// these compiles, and the promotion test above stays green for all of them:
//
//	Pool *pgxpool.Pool                                  // exported field
//	func (o *OrdersDB) PoolAccessor() *pgxpool.Pool     // accessor
//	func (o *OrdersDB) BeginTx(ctx) (pgx.Tx, error)     // a tx writes too
//	func (o *OrdersDB) RawPgConn(ctx) (*pgconn.PgConn, error)
//	func (o *OrdersDB) DBHandles() Handles              // wrapper holding one
//
// So the walk recurses: through pointers, slices, maps and channels, and into
// the exported fields of any struct it reaches. A handle three types deep is
// still a handle the caller can get to. Unexported fields are skipped at every
// level — the compiler already refuses those outside this package, which is
// the same division of labour as between these two tests.
//
// The list is of concrete handle types, so it is exact rather than complete:
// a method returning an interface that happens to carry Exec walks past.
// Extend the list rather than generalising it — a heuristic over method sets
// would start failing on the repo's own legitimate returns.
func TestOrdersDB_HandsOutNoWritableHandle(t *testing.T) {
	forbidden := map[reflect.Type]string{
		reflect.TypeOf((*pgxpool.Pool)(nil)):            "the pool itself",
		reflect.TypeOf((*pgxpool.Conn)(nil)):            "a pooled connection",
		reflect.TypeOf((*pgx.Conn)(nil)):                "a raw connection",
		reflect.TypeOf((*pgconn.PgConn)(nil)):           "a raw connection",
		reflect.TypeOf((*pgx.Tx)(nil)).Elem():           "a transaction",
		reflect.TypeOf((*pgx.BatchResults)(nil)).Elem(): "a batch to execute",
	}

	ptr := reflect.TypeOf(&OrdersDB{})

	for i := 0; i < ptr.Elem().NumField(); i++ {
		f := ptr.Elem().Field(i)
		if !f.IsExported() {
			continue
		}
		if where, what := reachableHandle(f.Type, forbidden, map[reflect.Type]bool{}); what != "" {
			t.Errorf("*OrdersDB.%s%s is %s, so anything holding the concrete type can "+
				"write SQL past the repo layer and skip its events", f.Name, where, what)
		}
	}

	for i := 0; i < ptr.NumMethod(); i++ {
		m := ptr.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			if where, what := reachableHandle(m.Type.Out(j), forbidden, map[reflect.Type]bool{}); what != "" {
				t.Errorf("*OrdersDB.%s()%s is %s, so its caller can write SQL past the "+
					"repo layer and skip its events — keep the handle inside this package "+
					"and export the operation instead", m.Name, where, what)
			}
		}
	}
}

// reachableHandle reports the first forbidden type reachable from t through
// exported structure, as the path a caller would write to get at it ("" when
// t is itself forbidden) and what it is. Both are empty when nothing is.
//
// seen breaks the cycle a self-referential type would otherwise cause; it is
// per-walk rather than shared, so a handle is still reported once per field
// and per method that leads to it.
func reachableHandle(t reflect.Type, forbidden map[reflect.Type]string, seen map[reflect.Type]bool) (where, what string) {
	if what, bad := forbidden[t]; bad {
		return "", what
	}
	if seen[t] {
		return "", ""
	}
	seen[t] = true

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return reachableHandle(t.Elem(), forbidden, seen)
	case reflect.Map:
		if where, what := reachableHandle(t.Key(), forbidden, seen); what != "" {
			return where, what
		}
		return reachableHandle(t.Elem(), forbidden, seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if where, what := reachableHandle(f.Type, forbidden, seen); what != "" {
				return "." + f.Name + where, what
			}
		}
	}
	return "", ""
}
