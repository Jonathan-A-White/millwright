package tmpdisk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRefusesADirectoryThatIsNotAGoTests(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "precious")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (Disk{}).Remove(other); err == nil {
		t.Error("removed a directory that is not a Test* one")
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("the directory is gone: %v", err)
	}
}

func TestEntriesFollowsNoLinkOutOfTheTempDir(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "TestLinked1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "node-compile-cache")); err != nil {
		t.Fatal(err)
	}
	entries, err := (Disk{}).Entries(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("listed %v through a link", entries)
	}
}
