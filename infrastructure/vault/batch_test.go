package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBatchesAreNumberedPerRigAndTheirFilesKeptUnderRunsBatch(t *testing.T) {
	ctx := context.Background()
	v := New(t.TempDir())
	for _, want := range []int{1, 2} {
		got, err := v.NewBatch(ctx, "lampas")
		if err != nil || got != want {
			t.Fatalf("expected batch %d of lampas, got %d (%v)", want, got, err)
		}
	}
	if got, err := v.NewBatch(ctx, "millwright"); err != nil || got != 1 {
		t.Fatalf("expected another rig's batches numbered from 1, got %d (%v)", got, err)
	}
	// A number that is not the next one's, left by hand, is passed.
	if err := os.Mkdir(filepath.Join(v.dir, "runs", "batch", "lampas", "7"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := v.NewBatch(ctx, "lampas"); err != nil || got != 8 {
		t.Fatalf("expected batch 8 after a 7, got %d (%v)", got, err)
	}

	rel, err := v.PutBatchFile(ctx, "lampas", 2, "outcome.txt", "landed abc\n")
	if err != nil || rel != "runs/batch/lampas/2/outcome.txt" {
		t.Fatalf("expected runs/batch/lampas/2/outcome.txt, got %q (%v)", rel, err)
	}
	if held, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(rel))); err != nil || string(held) != "landed abc\n" {
		t.Fatalf("expected the outcome kept, got %q (%v)", held, err)
	}
	for _, bad := range []string{"../x", ""} {
		if _, err := v.PutBatchFile(ctx, bad, 1, "outcome.txt", "x"); err == nil {
			t.Fatalf("expected a rig named %q refused", bad)
		}
	}
	if _, err := v.PutBatchFile(ctx, "lampas", 0, "outcome.txt", "x"); err == nil {
		t.Fatalf("expected batch 0 refused")
	}
}
