package rig_test

import (
	"os"
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
