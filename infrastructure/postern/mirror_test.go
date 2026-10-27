package postern_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestMillNamesListsOnlyTheRunsChangedSinceAndNeverTheCursor(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for name, at := range map[string]time.Time{"runs/old": now.Add(-time.Hour), "runs/fresh": now.Add(-time.Hour), "runs/inplace": now.Add(-time.Hour)} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, name, "timing.json")
		if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{file, filepath.Join(dir, name)} {
			if err := os.Chtimes(p, at, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "runs/fresh/answer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// rewritten in place: the run directory's own time does not move.
	if err := os.Chtimes(filepath.Join(dir, "runs/inplace/timing.json"), now, now); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"grinds.jsonl", application.MillCursorFile} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := postern.Mirrorer{}
	got, err := m.MillNames(context.Background(), dir, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if want := "grinds.jsonl runs/fresh runs/inplace"; strings.Join(got, " ") != want {
		t.Errorf("names = %q, want %q", strings.Join(got, " "), want)
	}
	all, err := m.MillNames(context.Background(), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(all)
	if want := "grinds.jsonl runs"; strings.Join(all, " ") != want {
		t.Errorf("whole names = %q, want %q", strings.Join(all, " "), want)
	}
}

func TestMillCursorRoundTripsAndAnUnreadableOneIsNone(t *testing.T) {
	dir := t.TempDir()
	m := postern.Mirrorer{}
	if _, ok, err := m.ReadMillCursor(context.Background(), dir); ok || err != nil {
		t.Fatalf("no file: ok=%v err=%v", ok, err)
	}
	at := time.Date(2026, 10, 27, 12, 0, 0, 5, time.UTC)
	if err := m.WriteMillCursor(context.Background(), dir, at); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := m.ReadMillCursor(context.Background(), dir); err != nil || !ok || !got.Equal(at) {
		t.Fatalf("got %v %v %v", got, ok, err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.MillCursorFile), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.ReadMillCursor(context.Background(), dir); ok || err != nil {
		t.Fatalf("junk: ok=%v err=%v", ok, err)
	}
}
