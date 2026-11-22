package doctor_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tellNames are the names a check's func field or method has when it only
// tells somebody: an Alarm to the Governor's phone, a Note or a Mail to the
// Mayor. A cure that sends one changes nothing on the host by that.
var tellNames = map[string]bool{
	"Alarm": true, "Clear": true, "Note": true, "Mail": true,
	"alarm": true, "note": true, "mail": true,
}

// TestEveryCureThatTellsSaysWhetherTheFaultStands is the one shared rule behind
// application.DoctorStandsAfterCure: a check whose Cure sends an Alarm, Note or
// Mail (directly, or through a method of its own type) must have a
// CureLeavesFault, or the doctor logs "cured" over a fault the cure only told
// somebody about (mw-6ww.185.31, mw-6ww.185.32, mw-6ww.185.33). A check whose
// cure also does real work says CureLeavesFault for the branch that only tells
// (the two-branch shape), so it has the method too.
func TestEveryCureThatTellsSaysWhetherTheFaultStands(t *testing.T) {
	var missing, noop []string
	for _, dir := range []string{".", filepath.Join("..", "..", "application")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		methods := map[string]map[string]*ast.FuncDecl{} // type -> method -> decl
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
					continue
				}
				typ := receiverName(fn.Recv.List[0].Type)
				if methods[typ] == nil {
					methods[typ] = map[string]*ast.FuncDecl{}
				}
				methods[typ][fn.Name.Name] = fn
			}
		}
		for typ, own := range methods {
			cure := own["Cure"]
			if cure == nil || own["CureLeavesFault"] != nil {
				continue
			}
			if tells(cure, own, map[string]bool{}) {
				missing = append(missing, dir+": "+typ)
			}
		}
		for typ, own := range methods {
			cure := own["Cure"]
			if cure == nil || own["CureLeavesFault"] != nil || !onlyReturnsNil(cure) {
				continue
			}
			if _, ok := noopCureLeftToAPerson[typ]; !ok {
				noop = append(noop, dir+": "+typ)
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("%s: Cure sends an alarm, note or mail but the type has no CureLeavesFault (application.DoctorStandsAfterCure)", m)
	}
	sort.Strings(noop)
	for _, m := range noop {
		t.Errorf("%s: Cure only returns nil and the type has no CureLeavesFault: a fault found would be logged cured with it standing (mw-6ww.185.36); add CureLeavesFault, or list the type in noopCureLeftToAPerson with its reason", m)
	}
}

// noopCureLeftToAPerson lists the checks whose Cure rightly does nothing at
// all and has no CureLeavesFault, each with its reason: a fault found stays
// for a person, and the doctor never calls Cure for it.
var noopCureLeftToAPerson = map[string]string{}

// onlyReturnsNil is whether fn's body is the one statement 'return nil'.
func onlyReturnsNil(fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	id, ok := ret.Results[0].(*ast.Ident)
	return ok && id.Name == "nil"
}

// tells is whether fn's body calls a tell, directly or through a method of the
// same type it calls on its receiver.
func tells(fn *ast.FuncDecl, own map[string]*ast.FuncDecl, seen map[string]bool) bool {
	if fn.Body == nil || seen[fn.Name.Name] {
		return false
	}
	seen[fn.Name.Name] = true
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if tellNames[sel.Sel.Name] {
			// A method of this type named alarm is followed, not counted: what it
			// calls decides. A func field named Alarm is the tell itself.
			if _, isMethod := own[sel.Sel.Name]; !isMethod {
				found = true
				return false
			}
		}
		if callee, isMethod := own[sel.Sel.Name]; isMethod && tells(callee, own, seen) {
			found = true
		}
		return !found
	})
	return found
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return receiverName(e.X)
	}
	return ""
}
