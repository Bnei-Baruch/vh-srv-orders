package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// noWritePath are the pool methods that cannot reach a write, and so cannot
// skip OrdersDB.emitEvent. They are still reported — a handler has no business
// holding the pool either way — but as a layering leak rather than a lost
// event.
//
// Listed this way round on purpose. The pgx v4 to v5 move deleted QueryFunc,
// BeginFunc and BeginTxFunc, and the list that named the write path carried all
// three for a while afterwards, matching nothing. Worse, a method a future pgx
// adds would have defaulted to the reassuring label. Inverted, an unrecognised
// method is treated as a write path until someone says otherwise, which is the
// direction that fails safe: over-warning on a read costs a sentence,
// under-warning on a write costs an event.
var noWritePath = map[string]bool{
	"Ping": true, "Stat": true, "Config": true, "Reset": true, "Close": true,
}

// repoDir is the package that declares OrdersDB. Its constructors are read from
// it rather than listed here, so a new one cannot be forgotten.
const repoDir = "../repo"

// The repo fields hold *repo.OrdersDB, which embeds *pgxpool.Pool, so a handler
// can write SQL directly and skip OrdersDB.emitEvent — the row changes, the
// request succeeds, and the event downstream consumers key off is never sent.
// The old interface-typed fields made that a compile error; this replaces the
// guardrail their removal deleted.
//
// Nothing here is keyed on a spelling. The banned method names are the method
// set of *pgxpool.Pool read by reflection, so a pgx bump cannot leave a stale
// list. The names that hold a pool are read from their declarations — named
// fields, embedded fields, parameters, locals, package-level vars, and types
// aliased to a pool type — because the eight handler structs still to come will
// each hold it their own way. Embedding matters most: it is how *OrdersDB holds
// the pool itself, and the bypass then reads as z.Exec(...), indistinguishable
// at the call site from a repo method.
//
// This guard is best-effort, deliberately. It answers "what type is this
// expression" by looking at how the expression is spelled, and that question has
// no syntactic answer, so the list below has been wrong before — six review
// rounds each found shapes the previous version had not imagined. Treat a green
// run as "no bypass in a shape we have seen", never as proof there is none.
//
// Doing it properly means go/types, which means golang.org/x/tools in the module
// graph. That was tried and backed out: it pulls x/crypto, x/net, x/sys and
// x/text up with it, so a test-only check would move the production dependency
// graph. Not worth it for a guard that exists to cover the gap until the
// consumer-side interfaces in the PR description make the bypass a compile error
// again. Those are the fix; this buys time.
//
// Known gaps, needing the type information above to close:
//
//   - a *pgxpool.Conn taken from Acquire: calls on the conn are not tracked,
//     which is why Acquire itself is treated as a write path;
//   - a pool reaching api through an interface, or from a helper in some third
//     package — the repo package's own constructors are read from it, so those
//     are covered;
//   - a defined type (type X repo.OrdersDB) rather than an alias, which does
//     not promote the pool's methods anyway.
//
// Test files are exempt: reading rows back is the point there, and a skipped
// event corrupts nothing. Close is exempt in app.go alone, matched on the exact
// base name, and the test pins that call site so this comment cannot drift.
func TestHandlersDoNotReachThePoolDirectly(t *testing.T) {
	banned := map[string]bool{}
	pt := reflect.TypeOf(&pgxpool.Pool{})
	for i := range pt.NumMethod() {
		banned[pt.Method(i).Name] = true
	}
	if len(banned) < 5 {
		t.Fatalf("reflected only %d methods off *pgxpool.Pool; the guard would pass vacuously", len(banned))
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	var files []*ast.File
	paths := map[*ast.File]string{}
	for _, pkg := range pkgs {
		for path, f := range pkg.Files {
			files = append(files, f)
			paths[f] = path
		}
	}

	s := &surface{
		types:   map[string]bool{"repo.OrdersDB": true, "pgxpool.Pool": true},
		fields:  map[string]bool{},
		structs: map[string]bool{},
		pkgVars: map[string]bool{},
		funcs:   map[string]bool{},
	}
	s.scanAliases(files) // first: a field may be declared through an alias
	s.scanDeclarations(files)
	s.promoteEmbedders(files) // a field whose type embeds the pool is a way in too
	if n := s.scanConstructors(t, fset); n == 0 {
		t.Fatalf("found no constructor returning *OrdersDB in %s; the guard would miss every pool taken from one", repoDir)
	}

	var offenders []string
	var closeSites []string

	for _, f := range files {
		path := paths[f]

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			// Per function, so a name in one body cannot condemn the same name
			// in another. Package-level vars are seeded in because that is the
			// scope they genuinely have.
			local := map[string]bool{}
			for name := range s.pkgVars {
				local[name] = true
			}
			s.bindReceiver(fn, local)
			s.bindParams(fn.Type, local)
			s.collectAliases(fn.Body, local)

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					s.bindParams(lit.Type, local)
					return true
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !banned[sel.Sel.Name] {
					return true
				}
				recv := chain(sel.X)
				switch {
				case s.reaches(recv, local):
				case recv == "" && s.yieldsPool(sel.X, local):
					// chain() renders nothing for a call, so name it here:
					// o.pool().Exec(...) reaches the pool as surely as o.repo does.
					if call, ok := sel.X.(*ast.CallExpr); ok {
						recv = chain(call.Fun) + "()"
					}
				default:
					return true
				}

				where := path + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
				what := recv + "." + sel.Sel.Name + "(...)"
				if noWritePath[sel.Sel.Name] {
					what += "  [layering, no write path]"
				}

				if sel.Sel.Name == "Close" && filepath.Base(path) == "app.go" {
					closeSites = append(closeSites, where)
					return true
				}
				offenders = append(offenders, where+"  "+what)
				return true
			})
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("handlers reached the connection pool directly. A call on the write path skips "+
			"the event every repo mutation emits; the rest leak the pool into the handler layer. "+
			"Add a method to *OrdersDB instead. Found at:\n  %s", strings.Join(offenders, "\n  "))
	}

	if len(closeSites) != 1 {
		t.Errorf("expected exactly one exempt Close() in app.go (Shutdown owns the pool), found %d: %v",
			len(closeSites), closeSites)
	}
}

// surface is what the package declares about where the pool lives.
type surface struct {
	types   map[string]bool // type expressions that are a pool, aliases included
	fields  map[string]bool // named struct fields holding one
	structs map[string]bool // struct types embedding one
	pkgVars map[string]bool // package-level vars holding one
	funcs   map[string]bool // functions in this package returning one
}

// scanConstructors records the functions in the repo package that hand back an
// OrdersDB, so a local taking one is tracked like any other alias. Reading them
// beats listing them: NewOrdersDBUrl was missing from the list this replaces.
func (s *surface) scanConstructors(t *testing.T, fset *token.FileSet) int {
	pkgs, err := parser.ParseDir(fset, repoDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", repoDir, err)
	}

	found := 0
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Type.Results == nil {
					continue
				}
				for _, r := range fn.Type.Results.List {
					rt := r.Type
					if star, ok := rt.(*ast.StarExpr); ok {
						rt = star.X
					}
					// Named from inside its own package, so no qualifier.
					if chain(rt) == "OrdersDB" {
						s.funcs[fn.Name.Name] = true
						found++
					}
				}
			}
		}
	}
	return found
}

func (s *surface) scanAliases(files []*ast.File) {
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if ok && ts.Assign.IsValid() && s.isPool(ts.Type) {
					s.types[ts.Name.Name] = true
				}
			}
		}
	}
}

func (s *surface) scanDeclarations(files []*ast.File) {
	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						st, ok := sp.Type.(*ast.StructType)
						if !ok || st.Fields == nil {
							continue
						}
						for _, fld := range st.Fields.List {
							if !s.isPool(fld.Type) {
								continue
							}
							if len(fld.Names) == 0 {
								// Embedded: the struct itself is the pool.
								s.structs[sp.Name.Name] = true
								continue
							}
							for _, name := range fld.Names {
								s.fields[name.Name] = true
							}
						}
					case *ast.ValueSpec:
						if d.Tok != token.VAR {
							continue
						}
						if s.isPool(sp.Type) {
							for _, name := range sp.Names {
								s.pkgVars[name.Name] = true
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Type.Results == nil {
					continue
				}
				for _, r := range d.Type.Results.List {
					if s.isPool(r.Type) {
						s.funcs[d.Name.Name] = true
					}
				}
			}
		}
	}
}

// promoteEmbedders propagates pool-ness outward: a field whose type embeds the
// pool is a way in, and a struct that embeds such a struct is one too. Both feed
// back into the scan, so it runs until nothing changes rather than a fixed
// number of times — a transitive embed is otherwise missed.
func (s *surface) promoteEmbedders(files []*ast.File) {
	for {
		changed := false
		for _, f := range files {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					for _, fld := range st.Fields.List {
						t := fld.Type
						if star, ok := t.(*ast.StarExpr); ok {
							t = star.X
						}
						if !s.structs[chain(t)] {
							continue
						}
						if len(fld.Names) == 0 {
							if !s.structs[ts.Name.Name] {
								s.structs[ts.Name.Name] = true
								changed = true
							}
							continue
						}
						for _, name := range fld.Names {
							if !s.fields[name.Name] {
								s.fields[name.Name] = true
								changed = true
							}
						}
					}
				}
			}
		}
		if !changed {
			return
		}
	}
}

// bindReceiver marks the receiver of a method on a struct that embeds the pool,
// where the bypass reads as z.Exec(...).
func (s *surface) bindReceiver(fn *ast.FuncDecl, local map[string]bool) {
	if fn.Recv == nil {
		return
	}
	for _, r := range fn.Recv.List {
		t := r.Type
		if star, ok := t.(*ast.StarExpr); ok {
			t = star.X
		}
		if !s.structs[chain(t)] {
			continue
		}
		for _, name := range r.Names {
			local[name.Name] = true
		}
	}
}

func (s *surface) bindParams(ft *ast.FuncType, local map[string]bool) {
	if ft.Params == nil {
		return
	}
	for _, p := range ft.Params.List {
		if !s.isPool(p.Type) {
			continue
		}
		for _, name := range p.Names {
			local[name.Name] = true
		}
	}
}

// collectAliases records locals handed the pool, twice over so an alias of an
// alias is seen.
func (s *surface) collectAliases(body *ast.BlockStmt, local map[string]bool) {
	for range 2 {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i < len(x.Lhs) && s.yieldsPool(rhs, local) {
						if id, ok := x.Lhs[i].(*ast.Ident); ok && id.Name != "_" {
							local[id.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				if s.isPool(x.Type) {
					for _, name := range x.Names {
						local[name.Name] = true
					}
				}
				for i, v := range x.Values {
					if i < len(x.Names) && s.yieldsPool(v, local) && x.Names[i].Name != "_" {
						local[x.Names[i].Name] = true
					}
				}
			}
			return true
		})
	}
}

// yieldsPool reports whether an expression evaluates to the pool — a selector
// onto it, or a call to something known to return one.
func (s *surface) yieldsPool(e ast.Expr, local map[string]bool) bool {
	if call, ok := e.(*ast.CallExpr); ok {
		c := chain(call.Fun)
		if i := strings.LastIndex(c, "."); i >= 0 {
			c = c[i+1:]
		}
		return s.funcs[c]
	}
	return s.reaches(chain(e), local)
}

// reaches reports whether a selector chain lands on the pool. Resolution is
// tried before trimming a trailing .Pool, so a field that is itself named Pool
// is not erased by the trim that exists for the embedded *pgxpool.Pool.
func (s *surface) reaches(c string, local map[string]bool) bool {
	for c != "" {
		if s.resolves(c, local) {
			return true
		}
		if !strings.HasSuffix(c, ".Pool") {
			return false
		}
		c = strings.TrimSuffix(c, ".Pool")
	}
	return false
}

// resolves looks a chain up without unwrapping anything. A bare identifier is
// looked up only in the function's own scope, never in the package-wide field
// set — two unrelated locals may share a name, and one of them being a field
// elsewhere says nothing about the other.
func (s *surface) resolves(c string, local map[string]bool) bool {
	if i := strings.LastIndex(c, "."); i >= 0 {
		return s.fields[c[i+1:]]
	}
	return local[c]
}

func (s *surface) isPool(e ast.Expr) bool {
	if e == nil {
		return false
	}
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	return s.types[chain(e)]
}

// chain renders a selector chain of plain identifiers, and "" for anything
// else — a call, an index, a literal.
func chain(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.ParenExpr:
		return chain(x.X)
	case *ast.SelectorExpr:
		p := chain(x.X)
		if p == "" {
			return ""
		}
		return p + "." + x.Sel.Name
	}
	return ""
}
