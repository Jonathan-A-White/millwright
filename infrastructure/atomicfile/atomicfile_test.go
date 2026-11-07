package atomicfile_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
)

// Two writers to one path at once, 50 times over, must both succeed and leave
// one whole file: the fixed "<path>.new" temp name this replaces made the late
// rename fail with "no such file" (mw-gq6.1032).
func TestTwoWritersToOnePathAtOnceBothSucceedAndLeaveAWholeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	a := bytes.Repeat([]byte("a"), 64<<10)
	b := bytes.Repeat([]byte("b"), 64<<10)

	for round := 0; round < 50; round++ {
		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, data := range [][]byte{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = atomicfile.Write(path, data, 0o644)
			}()
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d writer %d: %v", round, i, err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if !bytes.Equal(got, a) && !bytes.Equal(got, b) {
			t.Fatalf("round %d: file is not whole: %d bytes, starting %q", round, len(got), got[:min(len(got), 8)])
		}
	}
	assertOnlyFile(t, dir, "state")
}

func TestWriteSetsTheModeAndReplacesWhatWasThere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.WriteFile(path, []byte("old and longer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new\n" {
		t.Fatalf("got %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644", info.Mode().Perm())
	}
	assertOnlyFile(t, dir, "state")
}

func TestAFailedWriteLeavesNoTempFileAndKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	// A directory at the target makes the final rename fail, after the temp
	// file has been made and written.
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(path, []byte("x"), 0o644); err == nil {
		t.Fatal("want an error renaming over a directory")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "state" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteIntoAMissingDirectoryIsAnError(t *testing.T) {
	err := atomicfile.Write(filepath.Join(t.TempDir(), "nope", "state"), []byte("x"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("want an error naming the path, got %v", err)
	}
}

func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("want only %s in %s, got %v", name, dir, names)
	}
}
