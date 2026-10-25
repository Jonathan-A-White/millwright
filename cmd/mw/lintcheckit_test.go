package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestSourceFilesFindsTextUnderDirAndSkipsNodeModules(t *testing.T) {
	src := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("components/Card.tsx", "<button>Vote now</button>")
	write("node_modules/dep/index.js", "Only in a dependency")

	if found, err := (sourceFiles{}).Contains(src, "Vote now"); err != nil || !found {
		t.Fatalf("expected the label found, got %v %v", found, err)
	}
	if found, err := (sourceFiles{}).Contains(src, "Only in a dependency"); err != nil || found {
		t.Fatalf("expected node_modules left out, got %v %v", found, err)
	}
	if found, err := (sourceFiles{}).Contains(src, "Absent"); err != nil || found {
		t.Fatalf("expected an absent label not found, got %v %v", found, err)
	}
	if _, err := (sourceFiles{}).Contains(filepath.Join(src, "nope"), "x"); !errors.Is(err, application.ErrNoSource) {
		t.Fatalf("expected ErrNoSource, got %v", err)
	}
}
