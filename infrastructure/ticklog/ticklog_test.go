package ticklog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func read(t *testing.T, dir string) []string {
	t.Helper()
	held, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(held), "\n"), "\n")
}

func TestAppendMakesTheDirectoryAndTheFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "mw-millhand")
	if err := New(dir).Append(context.Background(), "2026-09-19T12:00:00Z quiet"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir); len(got) != 1 || got[0] != "2026-09-19T12:00:00Z quiet" {
		t.Fatalf("log = %q", got)
	}
}

func TestAppendKeepsLinesInOrder(t *testing.T) {
	dir := t.TempDir()
	log := New(dir)
	for _, line := range []string{"one", "two", "three"} {
		if err := log.Append(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(read(t, dir), ","); got != "one,two,three" {
		t.Fatalf("log = %q", got)
	}
}

func TestAppendCutsTheLogToItsLastLines(t *testing.T) {
	dir := t.TempDir()
	log := New(dir)
	total := application.TickLogLines + 37
	for i := 1; i <= total; i++ {
		if err := log.Append(context.Background(), fmt.Sprintf("line %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got := read(t, dir)
	if len(got) != application.TickLogLines {
		t.Fatalf("the log holds %d lines, want %d", len(got), application.TickLogLines)
	}
	if got[0] != fmt.Sprintf("line %d", total-application.TickLogLines+1) || got[len(got)-1] != fmt.Sprintf("line %d", total) {
		t.Fatalf("the log runs from %q to %q", got[0], got[len(got)-1])
	}
}

func TestAppendCutsAnOverlongLogInOneGo(t *testing.T) {
	dir := t.TempDir()
	var old strings.Builder
	for i := 1; i <= 900; i++ {
		fmt.Fprintf(&old, "old %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, File), []byte(old.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(dir).Append(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	got := read(t, dir)
	if len(got) != application.TickLogLines || got[0] != "old 402" || got[len(got)-1] != "new" {
		t.Fatalf("the log holds %d lines, from %q to %q", len(got), got[0], got[len(got)-1])
	}
}

func TestAppendLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := New(dir).Append(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != File {
		t.Fatalf("the directory holds %v", entries)
	}
}

func TestAppendFailsWhenTheDirectoryCannotBeMade(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New(filepath.Join(blocker, "sub")).Append(context.Background(), "one"); err == nil {
		t.Fatal("expected an error")
	}
}
