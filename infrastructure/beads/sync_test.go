package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// These tests never run the real `bd sync`: a real one would reach the
// factory's Dolt remote and publish whatever this machine happened to hold.
// They run a stand-in that exits the way bd documents instead, which is what
// the gateway has to be faithful about.

// standIn writes a program that says one thing and exits with one status, and
// returns a Gateway that runs it instead of bd.
func standIn(t *testing.T, says string, exit int) *beads.Gateway {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bd-stand-in")
	script := fmt.Sprintf("#!/bin/sh\necho %q >&2\nexit %d\n", says, exit)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, beads.WithProgram(path))
}

func TestSyncSurfacesTheExitCodeItWasGiven(t *testing.T) {
	for _, exit := range []int{1, 2, 3, 4, 7} {
		gateway := standIn(t, "bd said what it said", exit)

		err := gateway.Sync(context.Background())
		if err == nil {
			t.Fatalf("expected exit %d to be reported", exit)
		}
		halt, ok := application.Halted(err)
		if !ok {
			t.Fatalf("expected exit %d to come back as a halt, got %T: %v", exit, err, err)
		}
		if halt.Code != exit {
			t.Fatalf("expected the halt to carry exit %d, got %d", exit, halt.Code)
		}
		if !strings.Contains(halt.Error(), "bd said what it said") {
			t.Fatalf("expected the halt to repeat what bd said, got %q", halt.Error())
		}
		if halt.Transient() != (exit == 3) {
			t.Fatalf("expected only a lost push race to be transient, exit %d says %v", exit, halt.Transient())
		}
	}
}

func TestSyncSaysNothingWhenThereWasNothingWrong(t *testing.T) {
	if err := standIn(t, "nothing to do", 0).Sync(context.Background()); err != nil {
		t.Fatalf("expected a sync that worked to report nothing, got %v", err)
	}
}

func TestSyncReportsACommandThatNeverRan(t *testing.T) {
	gateway := beads.New(t.TempDir(), beads.WithProgram(filepath.Join(t.TempDir(), "no-such-bd")))

	err := gateway.Sync(context.Background())
	if err == nil {
		t.Fatal("expected a missing command to be reported")
	}
	if _, halted := application.Halted(err); halted {
		t.Fatalf("expected a command that never ran not to be called a halt, got %v", err)
	}
}

func TestANoteNeedsAKey(t *testing.T) {
	gateway := beads.New(t.TempDir())
	if _, err := gateway.Note(context.Background(), ""); err == nil {
		t.Fatal("expected a note with no key to be refused")
	}
	if err := gateway.SetNote(context.Background(), "", "now"); err == nil {
		t.Fatal("expected a note with no key to be refused")
	}
}
