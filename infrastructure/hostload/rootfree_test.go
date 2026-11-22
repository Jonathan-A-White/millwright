package hostload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
)

// mw-6ww.185.6: a reading carries the free space of the host's root filesystem, and
// leaves it unknown when it could not be read.
func TestProcReadsTheRootFilesystemsFreeSpace(t *testing.T) {
	dir := t.TempDir()
	load := filepath.Join(dir, "loadavg")
	if err := os.WriteFile(load, []byte("0.10 0.10 0.10 1/1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proc := hostload.Proc{File: load, Cores: 4, DriveFile: filepath.Join(dir, "none"), VersionFile: filepath.Join(dir, "none"),
		RootFree: func() (int64, error) { return 12 << 30, nil }}
	got, err := proc.Load(context.Background())
	if err != nil || !got.RootKnown || got.RootFreeBytes != 12<<30 {
		t.Fatalf("got RootKnown %v, RootFreeBytes %d, %v; want known, 12 GB", got.RootKnown, got.RootFreeBytes, err)
	}
	proc.RootFree = func() (int64, error) { return 0, errors.New("no statfs") }
	got, err = proc.Load(context.Background())
	if err != nil || got.RootKnown {
		t.Errorf("a root that could not be read: RootKnown %v, %v; want unknown and no error", got.RootKnown, err)
	}
}
