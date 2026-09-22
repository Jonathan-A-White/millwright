package synchalt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestReadOfNothingWrittenIsNoMark(t *testing.T) {
	marker := New(filepath.Join(t.TempDir(), "state", "mw"))
	if _, there, err := marker.Read(context.Background()); err != nil || there {
		t.Fatalf("expected no mark, there=%v, err=%v", there, err)
	}
}

func TestWriteMakesTheDirectoryAndReadReadsItBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "mw")
	marker := New(dir)
	at := time.Date(2026, 9, 22, 1, 35, 0, 0, time.UTC)
	info := application.SyncHaltInfo{At: at, Said: "conflict in the working set"}

	if err := marker.Write(context.Background(), info); err != nil {
		t.Fatalf("writing: %v", err)
	}
	got, there, err := marker.Read(context.Background())
	if err != nil || !there {
		t.Fatalf("reading back: there=%v, err=%v", there, err)
	}
	if !got.At.Equal(at) || got.Said != info.Said {
		t.Fatalf("expected %+v, got %+v", info, got)
	}
}

func TestWriteReplacesWhatWasThere(t *testing.T) {
	dir := t.TempDir()
	marker := New(dir)
	first := application.SyncHaltInfo{At: time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC), Said: "first"}
	second := application.SyncHaltInfo{At: time.Date(2026, 9, 22, 1, 30, 0, 0, time.UTC), Said: "second"}

	if err := marker.Write(context.Background(), first); err != nil {
		t.Fatalf("writing the first mark: %v", err)
	}
	if err := marker.Write(context.Background(), second); err != nil {
		t.Fatalf("writing the second mark: %v", err)
	}
	got, _, err := marker.Read(context.Background())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !got.At.Equal(second.At) || got.Said != second.Said {
		t.Fatalf("expected the mark replaced by %+v, got %+v", second, got)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one file left behind, got %v (err=%v)", entries, err)
	}
}

func TestClearRemovesTheMark(t *testing.T) {
	dir := t.TempDir()
	marker := New(dir)
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: time.Now(), Said: "x"}); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := marker.Clear(context.Background()); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if _, there, err := marker.Read(context.Background()); err != nil || there {
		t.Fatalf("expected no mark left, there=%v, err=%v", there, err)
	}
}

func TestClearingNothingIsNotAnError(t *testing.T) {
	marker := New(filepath.Join(t.TempDir(), "state", "mw"))
	if err := marker.Clear(context.Background()); err != nil {
		t.Fatalf("expected clearing nothing to be fine, got %v", err)
	}
}

func TestAMarkThatCannotBeParsedReadsAsNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte("garbage, not a time\n"), 0o644); err != nil {
		t.Fatalf("writing garbage: %v", err)
	}
	marker := New(dir)
	if _, there, err := marker.Read(context.Background()); err != nil || there {
		t.Fatalf("expected garbage to read as no mark, there=%v, err=%v", there, err)
	}
}
