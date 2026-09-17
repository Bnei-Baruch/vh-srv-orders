package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v4/pgxpool"
)

// The repo fields hold *repo.OrdersDB, which embeds *pgxpool.Pool, so a handler
// can write SQL directly and skip OrdersDB.emitEvent — the row changes, the
// request succeeds, and the event downstream consumers key off is never sent.
// The old interface-typed fields made that a compile error; this replaces the
// guardrail their removal deleted.
//
// Two things this does not do by hand. The banned names are the method set of
// *pgxpool.Pool read by reflection, not a list — a list omitted pgx's *Func
// helpers, and would go stale again on the next pgx bump. And the receiver is
// matched on the syntax tree rather than by regex, because `db := o.repo` and
// `o.repo.Pool.Exec(...)` both reach the pool while matching no pattern written
// against `.repo.`.
//
// Test files are exempt: reading rows back is the point there, and a skipped
// event corrupts nothing. Close is exempt in app.go only, where Shutdown owns
// the pool's lifetime; the test below pins that call site so this comment
// cannot drift away from what the code allows.
//
// If OrdersDB ever declares a method that shadows one of the pool's, the call
// stops being a bypass and this test will report it anyway. Allowlist it then.
func TestHandlersDoNotReachThePoolDirectly(t *testing.T) {
	banned := map[string]bool{}
	poolType := reflect.TypeOf(&pgxpool.Pool{})
	for i := range poolType.NumMethod() {
		banned[poolType.Method(i).Name] = true
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

	var offenders []string
	closeSites := []string{}

	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			aliases := poolAliases(file)

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !banned[sel.Sel.Name] {
					return true
				}
				if !reachesPool(chain(sel.X), aliases) {
					return true
				}

				where := path + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
				text := chain(sel.X) + "." + sel.Sel.Name + "(...)"

				if sel.Sel.Name == "Close" && strings.HasSuffix(path, "app.go") {
					closeSites = append(closeSites, where+"  "+text)
					return true
				}
				offenders = append(offenders, where+"  "+text)
				return true
			})
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("handlers reached the connection pool directly, which skips the event "+
			"every repo mutation emits — add a method to *OrdersDB instead. Found at:\n  %s",
			strings.Join(offenders, "\n  "))
	}

	if len(closeSites) != 1 {
		t.Errorf("expected exactly one exempt repo.Close() in app.go (Shutdown owns the pool), found %d: %v",
			len(closeSites), closeSites)
	}
}

var repoRooted = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.repo$`)

// poolAliases collects locals that were handed the pool, so that
// `db := o.repo` followed by `db.Exec(...)` is still caught.
func poolAliases(file *ast.File) map[string]bool {
	aliases := map[string]bool{}

	// Two passes: an alias can be assigned from an earlier alias.
	for range 2 {
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i < len(x.Lhs) && reachesPool(chain(rhs), aliases) {
						if id, ok := x.Lhs[i].(*ast.Ident); ok {
							aliases[id.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				for i, v := range x.Values {
					if i < len(x.Names) && reachesPool(chain(v), aliases) {
						aliases[x.Names[i].Name] = true
					}
				}
			}
			return true
		})
	}
	return aliases
}

// reachesPool reports whether a selector chain lands on the embedded pool:
// `o.repo`, any receiver name, `.Pool` named explicitly, or a local alias of
// either.
func reachesPool(c string, aliases map[string]bool) bool {
	if c == "" {
		return false
	}
	for strings.HasSuffix(c, ".Pool") {
		c = strings.TrimSuffix(c, ".Pool")
	}
	return repoRooted.MatchString(c) || aliases[c]
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
