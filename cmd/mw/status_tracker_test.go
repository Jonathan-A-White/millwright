package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tracker mw status reads from asks bd for the list of every bead once, for
// its snapshot, its bead graph and its stories since, and for the notes once
// (mw-gq6.659).
func TestStatusTrackerReadsEveryBeadAndEveryNoteOnce(t *testing.T) {
	bin, vault := t.TempDir(), t.TempDir()
	asked := filepath.Join(t.TempDir(), "asked.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + asked + "\nfor a in \"$@\"; do case \"$a\" in list) echo '[]'; exit 0;; kv) echo '{}'; exit 0;; sql) echo '[]'; exit 0;; esac; done\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"BEADS_DIR", "BEADS_DB", "BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_MODE"} {
		t.Setenv(name, "")
	}

	tracker := statusTracker(vault, "laptop")
	ctx := context.Background()
	if _, err := tracker.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.BeadGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.StoriesSince(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"hands.", "host.", ""} {
		if _, err := tracker.NotesWithPrefix(ctx, prefix); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(asked)
	if err != nil {
		t.Fatal(err)
	}
	var lists, kvs int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		switch {
		case strings.Contains(line, " list --all"):
			lists++
		case strings.Contains(line, " kv list"):
			kvs++
		}
	}
	if lists != 1 || kvs != 1 {
		t.Errorf("bd ran list --all %d times and kv list %d times, want once each:\n%s", lists, kvs, data)
	}
}

// mw-t5shinh.13: every mw status started a bd for its list of every bead (15
// bd starts and 6.8 s a run on 38,500 beads, 18.9 s as seats timed it). With a
// server to reach, the tracker reads that list, and the notes, over one
// connection kept open, and starts no bd.
func TestStatusTrackerReadsItsListingOverTheKeptConnectionAndStartsNoBd(t *testing.T) {
	asked := aBdThatRecords(t)
	server := aServerToReach(t)
	var said bytes.Buffer

	tracker, stop := keptStatusTracker(t.TempDir(), "laptop", &said)
	defer stop()
	ctx := context.Background()
	if _, err := tracker.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.BeadGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.StoriesSince(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	notes, err := tracker.NotesWithPrefix(ctx, "hands.")
	if err != nil || notes["hands.x"] != "a note" {
		t.Fatalf("expected the note the server held, got %v, %v", notes, err)
	}
	if got := bdsAsked(t, asked); got != 0 {
		t.Errorf("expected no bd started while the connection answers, got %d", got)
	}
	if got := server.queries("leases"); got != 1 {
		t.Errorf("expected the list of every bead read once, got %d reads", got)
	}
	if said.Len() != 0 {
		t.Errorf("expected nothing said, got %q", said.String())
	}
}

// With no server to reach the tracker reads by bd, as it did.
func TestStatusTrackerReadsByBdWhenThereIsNoServerToReach(t *testing.T) {
	asked := aBdThatRecords(t)
	var said bytes.Buffer

	tracker, stop := keptStatusTracker(t.TempDir(), "laptop", &said)
	defer stop()
	if _, err := tracker.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(asked)
	if !strings.Contains(string(data), " list --all") {
		t.Errorf("expected bd to list every bead, got:\n%s", data)
	}
	if said.Len() != 0 {
		t.Errorf("expected nothing said on a host with no server, got %q", said.String())
	}
}

// A kept read that fails mid-run leaves the rest of the run to bd, with the one
// line on stderr.
func TestStatusTrackerReadsByBdForTheRestOfARunWhoseKeptReadFailed(t *testing.T) {
	asked := aBdThatRecords(t)
	server := aServerToReach(t)
	var said bytes.Buffer

	tracker, stop := keptStatusTracker(t.TempDir(), "laptop", &said)
	defer stop()
	ctx := context.Background()
	if _, err := tracker.NotesWithPrefix(ctx, "hands."); err != nil {
		t.Fatal(err)
	}
	server.fail.Store(true)
	if _, err := tracker.Snapshot(ctx); err != nil {
		t.Fatalf("a failing connection must not fail the read: %v", err)
	}
	if _, err := tracker.BeadGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.NotesWithPrefix(ctx, "hands."); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(asked)
	if got := strings.Count(string(data), " list --all"); got != 1 {
		t.Errorf("expected the one bd list of every bead, got %d:\n%s", got, data)
	}
	if got := strings.Count(said.String(), keptTried); got != 1 {
		t.Errorf("expected the one line on stderr, got %d: %q", got, said.String())
	}
}
