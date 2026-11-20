package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goCacheFile writes one file of a fake Go build cache, last used age ago.
func goCacheFile(t *testing.T, dir, sub, name string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sub, name)
	if err := os.WriteFile(path, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	used := time.Now().Add(-age)
	if err := os.Chtimes(path, used, used); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// mw-gq6.1537: files of GOCACHE unused for over a day are deleted, newer ones kept.
func TestGoCacheTidyRemovesFilesUnusedForOverADayAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOCACHE", dir)
	oldAction := goCacheFile(t, dir, "ab", "abcdef01-a", 36*time.Hour)
	oldData := goCacheFile(t, dir, "ab", "abcdef01-d", 3*24*time.Hour)
	fresh := goCacheFile(t, dir, "cd", "cd012345-a", 2*time.Hour)
	dayEdge := goCacheFile(t, dir, "cd", "cd012345-d", 23*time.Hour)
	readme := goCacheFile(t, dir, "", "README", 30*24*time.Hour)
	trim := goCacheFile(t, dir, "", "trim.txt", 30*24*time.Hour)
	stranger := goCacheFile(t, dir, "ab", "notes.txt", 30*24*time.Hour)

	out, err := runMw(t, "host", "tidy", "go-cache")
	if err != nil {
		t.Fatalf("mw host tidy go-cache failed: %v\n%s", err, out)
	}
	for _, gone := range []string{oldAction, oldData} {
		if exists(gone) {
			t.Errorf("%s was unused for over a day and is still there", gone)
		}
	}
	for _, kept := range []string{fresh, dayEdge, readme, trim, stranger} {
		if !exists(kept) {
			t.Errorf("%s was removed, and is not a stale cache file", kept)
		}
	}
	if !strings.Contains(out, "removed 2 files") || !strings.Contains(out, "kept 2 files") {
		t.Errorf("the run does not say what it did: %q", out)
	}
}

func TestGoCacheTidyDryRunDeletesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOCACHE", dir)
	old := goCacheFile(t, dir, "ab", "abcdef01-a", 36*time.Hour)
	out, err := runMw(t, "host", "tidy", "go-cache", "--dry-run")
	if err != nil {
		t.Fatalf("failed: %v\n%s", err, out)
	}
	if !exists(old) {
		t.Errorf("--dry-run removed %s", old)
	}
	if !strings.Contains(out, "would remove 1 files") {
		t.Errorf("--dry-run does not say what it would do: %q", out)
	}
}

func TestGoCacheTidyOfACacheThatIsNotThereIsNoError(t *testing.T) {
	t.Setenv("GOCACHE", filepath.Join(t.TempDir(), "absent"))
	if out, err := runMw(t, "host", "tidy", "go-cache"); err != nil {
		t.Fatalf("failed: %v\n%s", err, out)
	}
}

// tmpEntry makes a file (or, for a name ending in "/", a directory holding one
// file) under dir, last touched age ago, and returns its path.
func tmpEntry(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	file := path
	if strings.HasSuffix(name, "/") {
		file = filepath.Join(path, "blob")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("leftover"), 0o644); err != nil {
		t.Fatal(err)
	}
	touched := time.Now().Add(-age)
	for _, p := range []string{file, filepath.Dir(file)} {
		if err := os.Chtimes(p, touched, touched); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// mw-d40q096: /tmp held 72 GB of Node's compile cache, 18 GB of Playwright's,
// and a Go test's dirs by the hundred; nothing took them away.
func TestTmpTidyRemovesWhatIsUntouchedForADayAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	oldNode := tmpEntry(t, dir, "node-compile-cache/v22/old.cache", 36*time.Hour)
	freshNode := tmpEntry(t, dir, "node-compile-cache/v22/fresh.cache", time.Hour)
	oldPlaywright := tmpEntry(t, dir, "playwright-transform-cache-1000/ab/old.js", 3*24*time.Hour)
	freshPlaywright := tmpEntry(t, dir, "playwright-transform-cache-1000/ab/fresh.js", 2*time.Hour)
	oldTest := tmpEntry(t, dir, "TestOneSettledPack123456/", 36*time.Hour)
	freshTest := tmpEntry(t, dir, "TestSyncRepacks987654/", time.Hour)
	// A test dir whose own mtime is old but which holds a file touched an hour ago.
	busyTest := tmpEntry(t, dir, "TestBusy111/", 36*time.Hour)
	busyFile := tmpEntry(t, dir, "TestBusy111/sub/live", time.Hour)
	stranger := tmpEntry(t, dir, "someone-elses-notes/", 30*24*time.Hour)
	testFile := tmpEntry(t, dir, "TestNotADir", 30*24*time.Hour)

	out, err := runMw(t, "host", "tidy", "tmp", "--dir", dir)
	if err != nil {
		t.Fatalf("mw host tidy tmp failed: %v\n%s", err, out)
	}
	for _, gone := range []string{oldNode, oldPlaywright, oldTest} {
		if exists(gone) {
			t.Errorf("%s was untouched for over a day and is still there", gone)
		}
	}
	for _, kept := range []string{freshNode, freshPlaywright, freshTest, busyTest, busyFile, stranger, testFile} {
		if !exists(kept) {
			t.Errorf("%s was removed, and is not stale", kept)
		}
	}
	if !strings.Contains(out, "removed 3 entries") || !strings.Contains(out, "kept 4 entries") {
		t.Errorf("the run does not say what it did: %q", out)
	}
}

func TestTmpTidyDryRunDeletesNothing(t *testing.T) {
	dir := t.TempDir()
	old := tmpEntry(t, dir, "TestOld1/", 36*time.Hour)
	out, err := runMw(t, "host", "tidy", "tmp", "--dir", dir, "--dry-run")
	if err != nil {
		t.Fatalf("failed: %v\n%s", err, out)
	}
	if !exists(old) {
		t.Errorf("--dry-run removed %s", old)
	}
	if !strings.Contains(out, "would remove 1 entries") {
		t.Errorf("--dry-run does not say what it would do: %q", out)
	}
}
