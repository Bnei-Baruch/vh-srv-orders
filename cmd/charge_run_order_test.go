package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// funcDecl parses billing.go and returns the named function or method. Parsed,
// not searched as text, so a comment mentioning a call is not a call.
func funcDecl(t *testing.T, name string) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "billing.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn, fset
		}
	}
	t.Fatalf("%s not found in billing.go — has it been renamed?", name)
	return nil, nil
}

// callName renders what a call calls: "f", "pkg.F", "x.y.M". Parentheses
// around the callee are unwrapped, so (utils.LogFatal)("x") is utils.LogFatal.
// Anything else (an index, a call result, a func literal) renders as "", which
// no allowlist holds.
func callName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x := callName(e.X); x != "" {
			return x + "." + e.Sel.Name
		}
	case *ast.ParenExpr:
		return callName(e.X)
	}
	return ""
}

// calls lists every call in node, in source order.
func calls(node ast.Node) []*ast.CallExpr {
	var found []*ast.CallExpr
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			found = append(found, call)
		}
		return true
	})
	return found
}

// Anything that can exit — the config check and every client constructor —
// must run before initBillingInfra, while there is nothing to drain. An exit
// after it would skip the callers' `defer cleanup()`: the pool stays open and
// NATS is dropped without draining.
func TestInitChargeRun_BuildsEverythingBeforeOpeningInfra(t *testing.T) {
	fn, _ := funcDecl(t, "initChargeRun")

	first := map[string]token.Pos{}
	for _, call := range calls(fn.Body) {
		if name := callName(call.Fun); first[name] == 0 {
			first[name] = call.Pos()
		}
	}

	infra := first["initBillingInfra"]
	if infra == 0 {
		t.Fatal("initChargeRun no longer calls initBillingInfra")
	}
	for _, before := range []string{"validateChargeConfig", "newChargeParts"} {
		if at := first[before]; at == 0 || at > infra {
			t.Errorf("initChargeRun must call %s before initBillingInfra", before)
		}
	}
}

// service is all that runs inside the cleanup, and it only assigns fields.
// Every call in it is on this list, however it is written: a call through a
// parenthesized, indexed or aliased callee renders as something not listed.
// A call that can exit belongs in newChargeParts or validateChargeConfig.
func TestChargeParts_ServiceOnlyWires(t *testing.T) {
	fn, fset := funcDecl(t, "service")

	allowed := map[string]bool{
		"p.resolver.SetManualDiscountProvider": true,
		"p.resolver.SetHHGrantProvider":        true,
		"p.resolver.SetCouponProvider":         true,
		"billing.NewBillingService":            true,
	}
	for _, call := range calls(fn.Body) {
		if name := callName(call.Fun); !allowed[name] {
			t.Errorf("%s: chargeParts.service calls %q, which runs after initBillingInfra: "+
				"build it in newChargeParts instead, or add it here only if it cannot exit",
				fset.Position(call.Pos()), name)
		}
	}
}
