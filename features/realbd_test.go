package features

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mw-gq6.1023: the scenarios run the bd a Builder's session names in
// MW_BD_REAL, not the mw bd script ahead of it on PATH.
func TestTheScenariosRunTheBdThatMwBdRuns(t *testing.T) {
	shim, real := t.TempDir(), t.TempDir()
	for _, dir := range []string{shim, real} {
		if err := os.WriteFile(filepath.Join(dir, "bd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir, err := realBdFirst(filepath.Join(real, "bd"))
	if err != nil {
		t.Fatalf("putting the real bd first: %v", err)
	}
	defer os.RemoveAll(dir)
	found, err := exec.LookPath("bd")
	if err != nil {
		t.Fatal(err)
	}
	if target, err := filepath.EvalSymlinks(found); err != nil || target != filepath.Join(real, "bd") {
		t.Errorf("bd on PATH is %s (%s, %v), want the real bd %s", found, target, err, filepath.Join(real, "bd"))
	}

	if _, err := realBdFirst(filepath.Join(real, "nothing")); err == nil {
		t.Errorf("a MW_BD_REAL that names no bd to run must be an error")
	}
	if dir, err := realBdFirst(""); dir != "" || err != nil {
		t.Errorf("with no MW_BD_REAL PATH is left alone, got %q, %v", dir, err)
	}
}
