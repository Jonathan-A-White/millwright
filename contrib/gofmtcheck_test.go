package contrib_test

// scripts/check-gofmt.sh, which make lint runs, refuses a tree where gofmt -l
// lists a Go file (mw-gq6.1156: two unformatted files reached main because
// nothing in the gate ran gofmt). These tests run it on throwaway directories,
// and on this repository.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkGofmt(t *testing.T, dir string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/check-gofmt.sh")
	if err != nil {
		t.Fatal(err)
	}
	out, err := command(t, "sh", script, dir).CombinedOutput()
	return string(out), err
}

func aTreeOf(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGofmtCheckPassesATreeOfFormattedFiles(t *testing.T) {
	dir := aTreeOf(t, map[string]string{"a.go": "package a\n\nfunc F() {}\n", "sub/b.go": "package sub\n"})
	if out, err := checkGofmt(t, dir); err != nil {
		t.Fatalf("expected a pass, got %v:\n%s", err, out)
	}
}

func TestGofmtCheckFailsNamingAnUnformattedFile(t *testing.T) {
	dir := aTreeOf(t, map[string]string{"a.go": "package a\n", "sub/bad.go": "package sub\nfunc  F( ) {\n}\n"})
	out, err := checkGofmt(t, dir)
	if err == nil {
		t.Fatalf("expected a failure, got a pass:\n%s", out)
	}
	if !strings.Contains(out, "sub/bad.go") {
		t.Fatalf("expected the failure to name sub/bad.go, got:\n%s", out)
	}
}

func TestGofmtCheckPassesThisRepository(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := checkGofmt(t, root); err != nil {
		t.Fatalf("this repository holds an unformatted Go file: %v\n%s", err, out)
	}
}
