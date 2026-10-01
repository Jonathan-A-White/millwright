package procs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/procs"
)

func TestCountIsTheNumberedEntriesWhoseNameIsTheHarnesss(t *testing.T) {
	proc := t.TempDir()
	for pid, comm := range map[string]string{"101": "claude\n", "102": "claude\n", "103": "bash\n", "104": "claude-sidecar\n"} {
		if err := os.MkdirAll(filepath.Join(proc, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proc, pid, "comm"), []byte(comm), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Not a process: no number; a process gone: no comm.
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "self", "comm"), []byte("claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "105"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := procs.Harness{Proc: proc}.Count(context.Background())
	if err != nil || got != 2 {
		t.Fatalf("counted %d (%v), want 2", got, err)
	}
}

func TestCountOfNoProcessTableIsAnError(t *testing.T) {
	if _, err := (procs.Harness{Proc: filepath.Join(t.TempDir(), "none")}).Count(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}
