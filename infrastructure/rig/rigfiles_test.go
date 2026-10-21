package rig_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestARigsFilesAreFoundAsFilesDirectoriesAndGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"src/todo/Board.tsx", "docs/a/b/c.md"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := rig.Files{}
	for pattern, want := range map[string]bool{
		"src/todo/Board.tsx": true,
		"src/todo/Gone.tsx":  false,
		"src/todo":           true,
		"src/todo/*.tsx":     true,
		"src/todo/*.ts":      false,
		"src/other/*.tsx":    false,
		"docs/**/*.md":       true,
		"nope/**/*.md":       false,
	} {
		if got := files.Exists(dir, pattern); got != want {
			t.Errorf("Exists(%q) = %v, want %v", pattern, got, want)
		}
	}
}

func TestADirectorysFilesAreCountedUpToOnePastTheMost(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"src/a.ts", "src/b.ts", "src/c/d.ts", "src/node_modules/x.js", "src/.cache/y", ".git/HEAD", "top.md"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := rig.Files{}
	for _, c := range []struct {
		dir   string
		most  int
		n     int
		isDir bool
	}{
		{"src", 20, 3, true},
		{"src", 1, 2, true},
		{"", 20, 4, true},
		{"src/a.ts", 20, 0, false},
		{"gone", 20, 0, false},
	} {
		if n, isDir := files.FilesIn(dir, c.dir, c.most); n != c.n || isDir != c.isDir {
			t.Errorf("FilesIn(%q, %d) = %d, %v; want %d, %v", c.dir, c.most, n, isDir, c.n, c.isDir)
		}
	}
}

func TestAGlobsFilesAreCountedUpToOnePastTheMost(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tests/e2e/a.ts", "tests/e2e/b.ts", "tests/e2e/c.json", "tests/e2e/sub/d.ts",
		"tests/e2e/node_modules/x.ts", "tests/unit/e.ts", "top.ts"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := rig.Files{}
	for _, c := range []struct {
		pattern string
		most    int
		n       int
	}{
		{"tests/e2e/*.ts", 20, 2},
		{"tests/e2e/*.ts", 1, 2},
		{"tests/**/*.ts", 20, 4},
		{"tests/*/*.ts", 20, 3},
		{"*.ts", 20, 1},
		{"gone/*.ts", 20, 0},
	} {
		if n := files.Matching(dir, c.pattern, c.most); n != c.n {
			t.Errorf("Matching(%q, %d) = %d; want %d", c.pattern, c.most, n, c.n)
		}
	}
}

func TestTestCallersAreTheTrackedTestFilesThatNameAStem(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"features/a.feature":      "open Settings\n",
		"tests/b.test.ts":         "import Settings\n",
		"src/c_test.go":           "// Settings\n",
		"src/screens/Settings.go": "Settings itself\n",
		"tests/other.test.ts":     "Reader only\n",
		"docs/notes.md":           "Settings in prose\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	files := rig.Files{}
	for _, c := range []struct {
		stems []string
		want  int
	}{
		{[]string{"Settings"}, 3},
		{[]string{"Settings", "Reader"}, 4},
		{[]string{"Nowhere"}, 0},
	} {
		got, err := files.TestCallers(dir, c.stems)
		if err != nil || got != c.want {
			t.Errorf("TestCallers(%q) = %d, %v; want %d", c.stems, got, err, c.want)
		}
	}
	if _, err := files.TestCallers(t.TempDir(), []string{"Settings"}); err == nil {
		t.Error("a directory that is no checkout was searched without an error")
	}
}
