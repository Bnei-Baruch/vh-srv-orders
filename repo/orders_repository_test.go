package repo

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
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
//	func (o *OrdersDB) Session() Session                // wrapper with an accessor
//	func (o *OrdersDB) PoolFunc() func() *pgxpool.Pool  // closure over one
//	func (o *OrdersDB) NewTx(ctx) (Tx, error)           // pgx.Tx, renamed
//	func (o *OrdersDB) Provider() PoolProvider          // interface handing
//	                                                    // one back
//	func (o *OrdersDB) Raw() *RawPool                   // defined over the
//	                                                    // pool's own struct
//
// So the walk recurses through everything that can carry a value out:
// pointers, slices, arrays, maps, channels, a func type's results, the
// exported fields of any struct it reaches, and the exported methods of every
// type it reaches — not only of *OrdersDB. A handle three types deep is still
// a handle the caller can get to.
//
// Unexported fields are skipped, but only when they are not embedded: an
// embedded unexported type promotes its exported fields across the package
// boundary, and the selector another package writes never names it. reflect
// reports such a field as unexported, because its name is the type's.
//
// An unexported field that is not embedded is genuinely the compiler's — the
// same division of labour as between these two tests. An exported accessor on
// that same type is not, which is why the method walk runs at every depth.
//
// Matching is by identity first, then two widenings that keep it exact:
// anything implementing one of the interface entries is that entry under
// another name, and a pointer convertible to one of the pointer entries is
// that handle after a one-token conversion.
//
// What still walks past: an interface nobody here names, carrying Exec by
// structure alone. Extend the list rather than generalising it — a heuristic
// over method sets would start failing on the repo's own legitimate returns.
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
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		sel := "." + f.Name
		if f.Anonymous {
			sel = "" // Promoted: the selector a caller writes skips the name.
		}
		if where, what := reachableHandle(f.Type, forbidden, map[reflect.Type]bool{}); what != "" {
			t.Errorf("*OrdersDB%s%s is %s, so anything holding the concrete type can "+
				"write SQL past the repo layer and skip its events", sel, where, what)
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

	for ft, what := range forbidden {
		// pgx.Tx under another name is still pgx.Tx, and so is any concrete
		// type satisfying it.
		if ft.Kind() == reflect.Interface && t.Implements(ft) {
			return "", what
		}
		// A defined type over the same underlying struct converts back in one
		// token: (*pgxpool.Pool)(o.repo.Raw()). Pointer conversion requires
		// identical underlying types, so this does not over-match.
		if t.Kind() == reflect.Pointer && ft.Kind() == reflect.Pointer && t.ConvertibleTo(ft) {
			return "", what
		}
	}

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		if where, what := reachableHandle(t.Elem(), forbidden, seen); what != "" {
			return where, what
		}
	case reflect.Map:
		if where, what := reachableHandle(t.Key(), forbidden, seen); what != "" {
			return where, what
		}
		if where, what := reachableHandle(t.Elem(), forbidden, seen); what != "" {
			return where, what
		}
	case reflect.Func:
		// Results only. A func that takes a pool hands nothing out.
		for i := 0; i < t.NumOut(); i++ {
			if where, what := reachableHandle(t.Out(i), forbidden, seen); what != "" {
				return "()" + where, what
			}
		}
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			// An embedded unexported type still promotes its exported fields
			// across the package boundary: the selector another package writes
			// never names the embedded type. reflect reports the field itself
			// as unexported, because its name is the type's.
			if !f.IsExported() && !f.Anonymous {
				continue
			}
			if where, what := reachableHandle(f.Type, forbidden, seen); what != "" {
				if f.Anonymous {
					// Promoted: the selector a caller writes skips the name.
					return where, what
				}
				return "." + f.Name + where, what
			}
		}
	}

	// Methods, of every type reached and not only of *OrdersDB. A type whose
	// own field is unexported can still have an exported accessor, and that
	// accessor crosses the boundary exactly as the top-level pass does.
	// Pointer receivers are in the method set of *T, value receivers in both.
	mt := t
	switch t.Kind() {
	case reflect.Pointer, reflect.Interface:
		// reflect exposes an interface's methods on the interface type itself;
		// a *pointer* to one has an empty method set, so wrapping it here
		// would walk nothing.
	default:
		mt = reflect.PointerTo(t)
	}
	for i := 0; i < mt.NumMethod(); i++ {
		m := mt.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			if where, what := reachableHandle(m.Type.Out(j), forbidden, seen); what != "" {
				return "." + m.Name + "()" + where, what
			}
		}
	}
	return "", ""
}

// The reflect walk above starts from a type, so it sees *OrdersDB's surface and
// nothing else. A package-level declaration hands the same pool across the same
// boundary and is invisible from there:
//
//	func PoolOf(o *OrdersDB) *pgxpool.Pool   // repo.PoolOf(o.repo).Exec(…)
//	var SharedPool *pgxpool.Pool             // repo.SharedPool.Exec(…)
//	type Handles struct{ Pool *pgxpool.Pool }
//	func NewHandles(o *OrdersDB) Handles     // repo.NewHandles(o.repo).Pool…
//
// This reads the package's own source for them, resolving the package's own
// named types as it goes — a free constructor's result is the bare identifier
// Handles, and the selector that makes it a finding is over in the type
// declaration. Nothing here needs a type checker: every name it resolves is
// declared in these files.
//
// It matches the spelling of an imported type rather than the type, so it is
// the weakest of the three guards. A type alias, an import under another
// alias, or a var whose type is inferred from its value all walk past — as
// does GetDBURL, which is exported on purpose and hands out a URL rather than
// a live handle.
//
// Being weak is tolerable; being wrong is not. A guard that fails a legitimate
// export teaches the next author to delete it, so the match is on the selector
// pair and pgx.TxOptions is not pgx.Tx.
//
// Spelling-matching is the right weight here anyway: resolving types properly
// means type-checking the package from a test inside it, which costs a
// dependency and seconds per run to catch shapes nobody writes by accident.
func TestRepoPackage_DeclaresNoPoolAccessor(t *testing.T) {
	spellings := map[string]bool{
		"pgxpool.Pool": true, "pgxpool.Conn": true, "pgx.Conn": true,
		"pgconn.PgConn": true, "pgx.Tx": true, "pgx.BatchResults": true,
	}

	names, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, names, "no sources found: the test must run in the package directory")

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		files = append(files, f)
	}

	// Named types are resolved through this rather than by a type checker: a
	// free constructor's result is the bare identifier Handles, and the
	// selector that makes it a finding is in the type declaration elsewhere.
	declared := map[string]ast.Expr{}
	for _, f := range files {
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, spec := range g.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					declared[ts.Name.Name] = ts.Type
				}
			}
		}
	}

	src := sourceWalk{spellings: spellings, declared: declared}

	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				// Methods are the reflect walk's; only free functions here.
				if d.Recv != nil || !d.Name.IsExported() || d.Type.Results == nil {
					continue
				}
				for _, r := range d.Type.Results.List {
					src.report(t, fset, d.Name.Name+"()", r.Type)
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || vs.Type == nil {
						continue
					}
					for _, n := range vs.Names {
						if n.IsExported() {
							src.report(t, fset, n.Name, vs.Type)
						}
					}
				}
			}
		}
	}
}

// sourceWalk is the reflect walk's shape over syntax: same question, same
// exported-only rule, asked of what leaves the package through a free function
// or a var rather than through *OrdersDB.
type sourceWalk struct {
	spellings map[string]bool
	declared  map[string]ast.Expr
}

func (w sourceWalk) report(t *testing.T, fset *token.FileSet, what string, expr ast.Expr) {
	t.Helper()

	where, node := w.find(expr, map[string]bool{})
	if node == nil {
		return
	}
	var rendered bytes.Buffer
	require.NoError(t, printer.Fprint(&rendered, fset, node))

	t.Errorf("%s:%d: repo.%s%s is %s, so any package importing repo can write SQL "+
		"past this layer and skip its events — keep the handle unexported and "+
		"export the operation instead",
		filepath.Base(fset.Position(expr.Pos()).Filename),
		fset.Position(expr.Pos()).Line, what, where, rendered.String())
}

// find returns the selector path a caller would write to reach a handle, and
// the node to name in the message — the outermost type that is entirely the
// handle, so a map of pools reports as the map. node is nil when there is none.
func (w sourceWalk) find(expr ast.Expr, seen map[string]bool) (where string, node ast.Expr) {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		// The pair, not the rendered text: a substring test reads pgx.Tx
		// inside pgx.TxOptions and fails a helper handing out a transaction
		// mode with a message about writing SQL.
		if pkg, ok := e.X.(*ast.Ident); ok {
			if qualified := pkg.Name + "." + e.Sel.Name; w.spellings[qualified] {
				return "", e
			}
		}
	case *ast.StarExpr:
		where, node := w.find(e.X, seen)
		return wrap(e, where, node)
	case *ast.ArrayType:
		where, node := w.find(e.Elt, seen)
		return wrap(e, where, node)
	case *ast.Ellipsis:
		where, node := w.find(e.Elt, seen)
		return wrap(e, where, node)
	case *ast.ChanType:
		where, node := w.find(e.Value, seen)
		return wrap(e, where, node)
	case *ast.MapType:
		if where, node := w.find(e.Key, seen); node != nil {
			return wrap(e, where, node)
		}
		where, node := w.find(e.Value, seen)
		return wrap(e, where, node)
	case *ast.FuncType:
		// Results only. A func that takes a pool hands nothing out.
		if e.Results != nil {
			for _, r := range e.Results.List {
				if where, node := w.find(r.Type, seen); node != nil {
					return "()" + where, node
				}
			}
		}
	case *ast.InterfaceType:
		for _, m := range e.Methods.List {
			ft, ok := m.Type.(*ast.FuncType)
			if !ok || ft.Results == nil || len(m.Names) == 0 {
				continue
			}
			for _, r := range ft.Results.List {
				if where, node := w.find(r.Type, seen); node != nil {
					return "." + m.Names[0].Name + "()" + where, node
				}
			}
		}
	case *ast.StructType:
		for _, f := range e.Fields.List {
			if len(f.Names) == 0 { // embedded: promotes across the boundary
				if where, node := w.find(f.Type, seen); node != nil {
					return where, node
				}
				continue
			}
			for _, n := range f.Names {
				if !n.IsExported() {
					continue // the compiler's half
				}
				if where, node := w.find(f.Type, seen); node != nil {
					return "." + n.Name + where, node
				}
			}
		}
	case *ast.Ident:
		// A type this package declares. Unexported ones count: another package
		// cannot name handles, but it can hold the value a free function
		// returns and select the exported field off it.
		if seen[e.Name] {
			return "", nil
		}
		seen[e.Name] = true
		if def, ok := w.declared[e.Name]; ok {
			return w.find(def, seen)
		}
	}
	return "", nil
}

// wrap names the whole type in the message when nothing was crossed to reach
// the handle: map[string]*pgxpool.Pool, not pgxpool.Pool.
func wrap(outer ast.Expr, where string, node ast.Expr) (string, ast.Expr) {
	if node != nil && where == "" {
		return "", outer
	}
	return where, node
}
