package contrib_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// mw-d40q096: a test that made its own directory in the system temp with
// os.MkdirTemp and never removed it left 0.9 GB a run in /tmp. A test makes its
// directories with t.TempDir(); a file that calls os.MkdirTemp (or
// ioutil.TempDir) with "" or "/tmp" as the parent must remove what it made, by
// os.RemoveAll or t.Cleanup. A parent that is a variable is a directory that is
// itself already tidied, and is not looked at.
func TestEveryTestDirMadeInTheSystemTempIsRemoved(t *testing.T) {
	var offenders []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "node_modules" || name == ".mw-worktrees" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		if madeInSystemTemp(file) > 0 && !removesWhatItMade(file) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("these test files make a directory in the system temp and never remove it (use t.TempDir, or os.RemoveAll in a t.Cleanup): %v", offenders)
	}
}

// TestTheSweepSeesATestThatLeavesItsDirectory keeps the sweep honest: a file
// that makes a directory in /tmp and removes nothing is named.
func TestTheSweepSeesATestThatLeavesItsDirectory(t *testing.T) {
	src := "package x\nimport (\"os\"; \"testing\")\nfunc TestX(t *testing.T) { os.MkdirTemp(\"\", \"x\") }\n"
	file, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if madeInSystemTemp(file) != 1 || removesWhatItMade(file) {
		t.Errorf("a MkdirTemp(\"\") with no RemoveAll should be an offender")
	}
}

// madeInSystemTemp counts the calls that make a directory directly in the
// system temp: os.MkdirTemp or ioutil.TempDir whose parent is "" or "/tmp".
func madeInSystemTemp(file *ast.File) int {
	n := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		pkg, isPkg := identName(sel)
		if !ok || !isPkg || !((pkg == "os" && sel.Sel.Name == "MkdirTemp") || (pkg == "ioutil" && sel.Sel.Name == "TempDir")) {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && (lit.Value == `""` || lit.Value == `"/tmp"`) {
			n++
		}
		return true
	})
	return n
}

func identName(sel *ast.SelectorExpr) (string, bool) {
	if sel == nil {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// removesWhatItMade is true when the file calls os.RemoveAll somewhere (in a
// t.Cleanup, a defer, or the caller of a helper that made the directory).
func removesWhatItMade(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if pkg, isPkg := identName(sel); isPkg && pkg == "os" && sel.Sel.Name == "RemoveAll" {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
