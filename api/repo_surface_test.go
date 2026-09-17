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

// poolTypes are the declared types that carry the connection pool: *OrdersDB
// embeds *pgxpool.Pool, so holding either reaches it.
var poolTypes = map[string]bool{"repo.OrdersDB": true, "pgxpool.Pool": true}

// mutating are the pool methods that write, and so skip OrdersDB.emitEvent.
// The rest of the pool's surface is reported too, but as a layering leak, not
// as a lost event — Ping does not skip anything.
var mutating = map[string]bool{
	"Exec": true, "CopyFrom": true, "SendBatch": true,
	"Begin": true, "BeginTx": true, "BeginFunc": true, "BeginTxFunc": true,
	"Query": true, "QueryRow": true, "QueryFunc": true,
}

// The repo fields hold *repo.OrdersDB, which embeds *pgxpool.Pool, so a handler
// can write SQL directly and skip OrdersDB.emitEvent — the row changes, the
// request succeeds, and the event downstream consumers key off is never sent.
// The old interface-typed fields made that a compile error; this replaces the
// guardrail their removal deleted.
//
// Three things are deliberately not hand-written. The banned names are the
// method set of *pgxpool.Pool read by reflection, so a pgx bump cannot leave a
// stale list behind. The receiver is matched on the syntax tree, so an alias is
// visible. And the *names* that hold a pool are read from their declarations —
// struct fields, parameters, locals — rather than assumed to be `repo`, because
// the eight handler structs still to come will each name their own field.
//
// What this still cannot see, for want of type information: a pool that arrives
// through an interface, or a field whose name collides with a non-pool field of
// the same name elsewhere in the package. Both need go/types; neither is
// reachable in api/ today.
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

	// Package-wide: any struct field declared with a pool type, whatever it is
	// called. This is what makes the guard survive the next handler struct.
	poolFields := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, fld := range st.Fields.List {
				if !isPoolType(fld.Type) {
					continue
				}
				for _, name := range fld.Names {
					poolFields[name.Name] = true
				}
			}
			return true
		})
	}

	var offenders []string
	var closeSites []string

	for _, f := range files {
		path := paths[f]

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}

			// Locals and parameters are collected per function, so an alias in
			// one body cannot condemn the same name in another.
			local := map[string]bool{}
			if fn.Type.Params != nil {
				for _, p := range fn.Type.Params.List {
					if isPoolType(p.Type) {
						for _, name := range p.Names {
							local[name.Name] = true
						}
					}
				}
			}
			collectAliases(fn.Body, poolFields, local)

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !banned[sel.Sel.Name] {
					return true
				}
				recv := chain(sel.X)
				if !reachesPool(recv, poolFields, local) {
					return true
				}

				where := path + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
				what := recv + "." + sel.Sel.Name + "(...)"
				if !mutating[sel.Sel.Name] {
					what += "  [layering, not a lost event]"
				}

				if sel.Sel.Name == "Close" && filepath.Base(path) == "app.go" {
					closeSites = append(closeSites, where)
					return true
				}
				offenders = append(offenders, where+"  "+what)
				return true
			})
			return true
		})
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("handlers reached the connection pool directly. A mutating call skips the "+
			"event every repo mutation emits; the rest leak the pool into the handler layer. "+
			"Add a method to *OrdersDB instead. Found at:\n  %s", strings.Join(offenders, "\n  "))
	}

	if len(closeSites) != 1 {
		t.Errorf("expected exactly one exempt Close() in app.go (Shutdown owns the pool), found %d: %v",
			len(closeSites), closeSites)
	}
}

// collectAliases records locals handed the pool, twice over so that an alias of
// an alias is seen.
func collectAliases(body *ast.BlockStmt, poolFields, local map[string]bool) {
	for range 2 {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i < len(x.Lhs) && reachesPool(chain(rhs), poolFields, local) {
						if id, ok := x.Lhs[i].(*ast.Ident); ok {
							local[id.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				if isPoolType(x.Type) {
					for _, name := range x.Names {
						local[name.Name] = true
					}
				}
				for i, v := range x.Values {
					if i < len(x.Names) && reachesPool(chain(v), poolFields, local) {
						local[x.Names[i].Name] = true
					}
				}
			}
			return true
		})
	}
}

// reachesPool reports whether a selector chain lands on the pool: a local or
// parameter that holds one, or any chain whose final field is pool-typed —
// o.repo, a.ordersAPI.repo, x.db — with .Pool named explicitly or not.
func reachesPool(c string, poolFields, local map[string]bool) bool {
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
		return poolFields[c[i+1:]]
	}
	return local[c] || poolFields[c]
}

// isPoolType reports whether a type expression is *repo.OrdersDB or
// *pgxpool.Pool, named or embedded.
func isPoolType(e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	return poolTypes[chain(e)]
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
