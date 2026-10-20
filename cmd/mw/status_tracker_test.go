package main

import (
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
