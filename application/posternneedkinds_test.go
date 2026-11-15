package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// TestPosternNeedKindsListsEveryConstant reads posternview.go and holds
// PosternNeedKinds to the PosternNeed constants declared there: a new kind
// that is not listed, or listed twice, fails here.
func TestPosternNeedKindsListsEveryConstant(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "posternview.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "PosternNeed") || i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			declared = append(declared, v)
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("found no PosternNeed constant in posternview.go")
	}
	for _, v := range declared {
		if !slices.Contains(application.PosternNeedKinds, v) {
			t.Errorf("%q is a PosternNeed constant that PosternNeedKinds lacks", v)
		}
	}
	seen := map[string]bool{}
	for _, v := range application.PosternNeedKinds {
		if seen[v] {
			t.Errorf("PosternNeedKinds lists %q twice", v)
		}
		seen[v] = true
		if !slices.Contains(declared, v) {
			t.Errorf("PosternNeedKinds lists %q, which no PosternNeed constant has", v)
		}
	}
}
