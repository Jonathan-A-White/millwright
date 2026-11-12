package postern_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

func TestNeedsCountsFileReadsNilWhenMissingAndTheBytesWhenThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "needs-counts.json")
	file := postern.NewNeedsCountsFile(path)
	if raw, err := file.ReadNeedsCounts(context.Background()); err != nil || raw != nil {
		t.Fatalf("missing file: got %q, %v, want nil", raw, err)
	}
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if raw, err := file.ReadNeedsCounts(context.Background()); err != nil || string(raw) != `{"v":1}` {
		t.Fatalf("got %q, %v", raw, err)
	}
}
