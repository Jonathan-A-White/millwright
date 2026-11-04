package config_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// rootReaders are the functions of this package that read a key of the config
// file's root table, and the place of the key among their arguments.
var rootReaders = map[string]int{
	"optionalSetting": 0,
	"fullPathSetting": 0,
	"setting":         0,
	"valueIn":         1,
}

// TestEveryRootKeyThePackageReadsIsOneThatMwConfigKnows fails when a key is
// read from the file's root table but missing from domain.KnownRootConfigKeys,
// which is what mw config set and get refuse a key by (mw-gq6.964).
func TestEveryRootKeyThePackageReadsIsOneThatMwConfigKnows(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	read := map[string]string{}
	fset := token.NewFileSet()
	for _, name := range files {
		if filepath.Ext(name) != ".go" || len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			at, reads := rootReaders[fn.Name]
			if !reads || len(call.Args) <= at {
				return true
			}
			if lit, ok := call.Args[at].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if key, err := strconv.Unquote(lit.Value); err == nil {
					read[key] = name
				}
			}
			return true
		})
	}
	if len(read) < 20 {
		t.Fatalf("found only %d keys read (%v): the scan no longer sees the readers", len(read), read)
	}
	for key, name := range read {
		if !slices.Contains(domain.KnownRootConfigKeys, key) {
			t.Errorf("%s reads the root key %q, which is not in domain.KnownRootConfigKeys: mw config set and get would refuse it", name, key)
		}
	}
}
