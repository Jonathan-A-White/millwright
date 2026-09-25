package postern_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

func TestSnapshotFileWritesAtomicallyLeavingNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "snapshot.bin")
	file := postern.NewSnapshotFile(path)

	if err := file.Write(context.Background(), []byte("first")); err != nil {
		t.Fatalf("writing the snapshot: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the snapshot back: %v", err)
	}
	if string(raw) != "first" {
		t.Fatalf("expected the snapshot to hold %q, got %q", "first", raw)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no temp file left behind, stat gave: %v", err)
	}

	if err := file.Write(context.Background(), []byte("second")); err != nil {
		t.Fatalf("writing the snapshot again: %v", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the snapshot back: %v", err)
	}
	if string(raw) != "second" {
		t.Fatalf("expected the second write to replace the first, got %q", raw)
	}
	if file.Path() != path {
		t.Fatalf("expected Path() to report %q, got %q", path, file.Path())
	}
}
