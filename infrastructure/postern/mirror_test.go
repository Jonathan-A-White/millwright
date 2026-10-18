package postern_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// standInRsync puts an rsync on PATH that prints stderr and exits with status.
// It sets PATH, so the test cannot be parallel.
func standInRsync(t *testing.T, status int, stderr string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\ncat >&2 <<'EOF'\n" + stderr + "\nEOF\nexit " + strconv.Itoa(status) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "rsync"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func copyOnce(t *testing.T) error {
	t.Helper()
	return postern.Mirrorer{}.Copy(context.Background(), t.TempDir(), []string{"blobs"}, nil, "desktop:/data/")
}

func TestCopyTakesRsyncExit24ForFilesVanishedAndCountsThem(t *testing.T) {
	standInRsync(t, 24, "file has vanished: \"/data/blobs/7779\"\nfile has vanished: \"/data/blobs/778a\"\nrsync warning: some files vanished before they could be transferred (code 24) at main.c(1338) [sender=3.2.7]")
	var vanished *application.CopyVanished
	err := copyOnce(t)
	if !errors.As(err, &vanished) || vanished.Files != 2 {
		t.Fatalf("err = %v, want CopyVanished of 2 files", err)
	}
}

func TestCopyCountsAtLeastOneFileWhenExit24NamesNone(t *testing.T) {
	standInRsync(t, 24, "rsync warning: some files vanished before they could be transferred (code 24)")
	var vanished *application.CopyVanished
	if err := copyOnce(t); !errors.As(err, &vanished) || vanished.Files != 1 {
		t.Fatalf("err = %v, want CopyVanished of 1 file", err)
	}
}

func TestCopyStillFailsOnRsyncExit23AndOtherCodes(t *testing.T) {
	for _, status := range []int{23, 12, 30, 255} {
		standInRsync(t, status, "rsync: something went wrong")
		err := copyOnce(t)
		var vanished *application.CopyVanished
		if err == nil || errors.As(err, &vanished) || !strings.Contains(err.Error(), "rsync to desktop:/data/") {
			t.Fatalf("exit %d: err = %v, want a plain failure", status, err)
		}
	}
}

func TestCopySucceedsOnRsyncExit0(t *testing.T) {
	standInRsync(t, 0, "")
	if err := copyOnce(t); err != nil {
		t.Fatal(err)
	}
}
