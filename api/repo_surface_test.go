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

	"github.com/jackc/pgx/v4/pgxpool"
)

// writePath are the pool methods that can reach a write, and so can skip
// OrdersDB.emitEvent. Query and friends are here because a CTE or a
// SELECT ... FOR UPDATE writes; the Acquire family is here because the
// *pgxpool.Conn it hands back is the most direct way to run an unguarded Exec.
// Everything else on the pool — Ping, Stat, Config, Reset — loses no event and
// is reported as a layering leak instead.
var writePath = map[string]bool{
	"Exec": true, "CopyFrom": true, "SendBatch": true,
	"Begin": true, "BeginTx": true, "BeginFunc": true, "BeginTxFunc": true,
	"Query": true, "QueryRow": true, "QueryFunc": true,
	"Acquire": true, "AcquireFunc": true, "AcquireAllIdle": true,
}

// poolReturning are functions known to hand back a pool, so that a local taking
// their result is tracked like any other alias.
var poolReturning = map[string]bool{"NewOrdersDB": true}

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
// Known gaps, all of which need go/types to close:
//
//   - a *pgxpool.Conn taken from Acquire: calls on the conn are not tracked,
//     which is why Acquire itself is treated as a write path;
//   - a pool reaching api through an interface, or from a helper outside this
//     package that is not in poolReturning;
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
				if !s.reaches(recv, local) {
					return true
				}

				where := path + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
				what := recv + "." + sel.Sel.Name + "(...)"
				if !writePath[sel.Sel.Name] {
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

// promoteEmbedders marks fields and vars whose type is a struct that embeds the
// pool, so b.a.Exec(...) is caught when a's type embeds it. Run to a fixpoint,
// since such a struct may itself be held by another.
func (s *surface) promoteEmbedders(files []*ast.File) {
	for range 3 {
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				st, ok := n.(*ast.StructType)
				if !ok || st.Fields == nil {
					return true
				}
				for _, fld := range st.Fields.List {
					t := fld.Type
					if star, ok := t.(*ast.StarExpr); ok {
						t = star.X
					}
					if !s.structs[chain(t)] {
						continue
					}
					for _, name := range fld.Names {
						s.fields[name.Name] = true
					}
				}
				return true
			})
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
		return s.funcs[c] || poolReturning[c]
	}
	return s.reaches(chain(e), local)
}

// reaches reports whether a selector chain lands on the pool. A bare identifier
// is looked up only in the function's own scope, never in the package-wide
// field set — two unrelated locals may share a name, and one of them being a
// field elsewhere says nothing about the other.
func (s *surface) reaches(c string, local map[string]bool) bool {
	if c == "" {
		return false
	}
	for strings.HasSuffix(c, ".Pool") {
		c = strings.TrimSuffix(c, ".Pool")
	}
	if c == "" {
		return false
	}
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
