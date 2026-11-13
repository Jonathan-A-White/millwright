package application

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestTimeNowOnlyInANowDefault holds that these readers do not read the host's
// real clock and then replace it when their injected Now is set (mw-gq6.1244):
// a time.Now() call lies only in a method named now that returns it when Now is
// nil.
func TestTimeNowOnlyInANowDefault(t *testing.T) {
	for _, file := range []string{"pauseusage.go", "pause.go", "followerstall.go", "queue.go"} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name == "now" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Now" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" {
					t.Errorf("%s: %s reads time.Now() outside a now() default", fset.Position(call.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
}
