package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// kvStandIn is a stand-in bd whose `kv get` says the value in its file (or
// that the key does not exist) and writes down every call, so a test counts
// the reads that reached bd.
func kvStandIn(t *testing.T) (program, calls, value string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls.log")
	value = filepath.Join(dir, "value")
	program = filepath.Join(dir, "bd-kv")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
case "$*" in
*"kv get"*)
  if [ -f %s ]; then cat %s; else echo "key not found" >&2; exit 1; fi ;;
esac
exit 0
`, calls, value, value)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, calls, value
}

// The follower reads the same few notes every pass (the month's review filed,
// the harness hold, the inbox cursor): a Gateway told to keep notes reads each
// from bd once in a wait, and the reads in between are answered from memory.
func TestNotesAreReadFromBdOnceInAWait(t *testing.T) {
	program, calls, value := kvStandIn(t)
	if err := os.WriteFile(value, []byte("filed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := timeZero()
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithClock(func() time.Time { return now }),
		beads.WithNoteCache(time.Minute))

	for i := 0; i < 20; i++ {
		got, err := g.Note(context.Background(), "contribute-back.2026-10")
		if err != nil || got != "filed" {
			t.Fatalf("read %d: %q, %v", i, got, err)
		}
	}
	if got := startedCount(t, calls); got != 1 {
		t.Fatalf("expected 20 reads of a note to reach bd once, got %d", got)
	}

	now = now.Add(time.Minute)
	if _, err := g.Note(context.Background(), "contribute-back.2026-10"); err != nil {
		t.Fatal(err)
	}
	if got := startedCount(t, calls); got != 2 {
		t.Fatalf("expected a read after the wait to reach bd again, got %d calls", got)
	}
}

// A note that is not there is remembered as not there too, for it is the
// answer the follower gets most.
func TestAMissingNoteIsRemembered(t *testing.T) {
	program, calls, _ := kvStandIn(t)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithNoteCache(time.Minute))

	for i := 0; i < 5; i++ {
		if got, err := g.Note(context.Background(), "host.laptop.harness_hold"); err != nil || got != "" {
			t.Fatalf("read %d: %q, %v", i, got, err)
		}
	}
	if got := startedCount(t, calls); got != 1 {
		t.Fatalf("expected one bd for five reads of a missing note, got %d", got)
	}
}

// What this Gateway writes it reads back at once: a note set or cleared is the
// note's value now, not what was kept before.
func TestANoteThisGatewayWritesIsReadBackAtOnce(t *testing.T) {
	program, calls, value := kvStandIn(t)
	if err := os.WriteFile(value, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithNoteCache(time.Hour))

	if got, _ := g.Note(context.Background(), "k"); got != "old" {
		t.Fatalf("expected old, got %q", got)
	}
	if err := g.SetNote(context.Background(), "k", "new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := g.Note(context.Background(), "k"); got != "new" {
		t.Fatalf("expected the note this gateway set, got %q", got)
	}
	if err := g.ClearNote(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	if got, _ := g.Note(context.Background(), "k"); got != "" {
		t.Fatalf("expected the cleared note to be empty, got %q", got)
	}
	_ = calls
}

// A Gateway not told to keep notes reads every one from bd, as every command
// other than the follower does.
func TestWithoutTheCacheEveryNoteIsReadFromBd(t *testing.T) {
	program, calls, value := kvStandIn(t)
	if err := os.WriteFile(value, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := beads.New(t.TempDir(), beads.WithProgram(program))
	for i := 0; i < 3; i++ {
		if _, err := g.Note(context.Background(), "k"); err != nil {
			t.Fatal(err)
		}
	}
	if got := startedCount(t, calls); got != 3 {
		t.Fatalf("expected 3 reads to reach bd, got %d", got)
	}
}
