package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNoteAppendsALineToTheSeatsReaperLog(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	ctx := context.Background()

	for _, line := range []string{"2026-09-19T12:00:00Z reap @3: armed", "2026-09-19T12:01:30Z reap @3: closed\n"} {
		if err := v.Note(ctx, "mayor", line); err != nil {
			t.Fatalf("noting %q: %v", line, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, ".mayor-reaper.log"))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	want := "2026-09-19T12:00:00Z reap @3: armed\n2026-09-19T12:01:30Z reap @3: closed\n"
	if string(got) != want {
		t.Errorf("expected the two lines one after the other:\n%s\ngot:\n%s", want, got)
	}
}

func TestNoteWritesOneLineAndOnlyInsideTheVault(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	ctx := context.Background()

	if err := v.Note(ctx, "mayor", "two\nlines"); err == nil {
		t.Errorf("expected a line holding a newline to be refused")
	}
	if err := v.Note(ctx, "../mayor", "armed"); err == nil {
		t.Errorf("expected a seat that reaches outside the vault to be refused")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("expected a refused note to leave the vault as it was, got %v", entries)
	}
}

func TestWriteActingReplacesTheActingFileWholeAndOnlyInsideTheVault(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	ctx := context.Background()

	for _, text := range []string{"Mayor after handoff 159, a long first line\n", "Mayor mayor-160 (handed over at event 7)\n"} {
		if err := v.WriteActing(ctx, "mayor", text); err != nil {
			t.Fatalf("writing %q: %v", text, err)
		}
		got, err := os.ReadFile(filepath.Join(dir, ".mayor-acting"))
		if err != nil || string(got) != text {
			t.Fatalf("the acting file holds %q, %v; want %q", got, err, text)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".mayor-acting.*")); len(left) != 0 {
		t.Errorf("a temporary file was left behind: %v", left)
	}
	if err := v.WriteActing(ctx, "../mayor", "x"); err == nil {
		t.Errorf("expected a seat that reaches outside the vault to be refused")
	}
}
