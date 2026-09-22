package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
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

// standInThatClearsOnItsSecondCall returns a Gateway whose stand-in exits with
// first on the first call and 0 on every one after, counting calls in a file
// beside it — the shape of a conflict that bd itself would have cleared by
// the following try, as one did on the Mayor's boot sync.
func standInThatClearsOnItsSecondCall(t *testing.T, first int, saidFirst string) *beads.Gateway {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	path := filepath.Join(dir, "bd-stand-in")
	// Only `bd sync` itself is made to fail and counted; `kv get`/`kv set`,
	// which Note and SetNote also run through this same stand-in, always
	// succeed, or the note-keeping around the cycle would be what failed.
	script := fmt.Sprintf(`#!/bin/sh
is_sync=0
for a in "$@"; do
  if [ "$a" = "sync" ]; then
    is_sync=1
  fi
done
if [ "$is_sync" = "0" ]; then
  exit 0
fi
n=$(cat %q 2>/dev/null || echo 0)
n=$((n+1))
echo "$n" > %q
if [ "$n" = "1" ]; then
  echo %q >&2
  exit %d
fi
exit 0
`, counter, counter, saidFirst, first)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, beads.WithProgram(path))
}

// A conflict that clears by the next try is exactly what motivated this
// story: `mw sync` gave application.Sync.Run the retry, and the real gateway
// underneath is what actually runs bd a second time — this is the two working
// together, not the fake tracker standing in for one of them.
func TestASyncConflictThatClearsOnRetryGoesThroughTheRealGateway(t *testing.T) {
	gateway := standInThatClearsOnItsSecondCall(t, 2, "CONFLICT in the working set")
	sync := application.Sync{
		Vault:   &apptest.FakeVaultFiles{},
		Tracker: gateway,
		Host:    "vps",
		Sleep:   func(context.Context, time.Duration) error { return nil },
	}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the conflict to clear on the gateway's second bd sync, got %v", err)
	}
	if want := "conflict cleared on retry (bd said: CONFLICT in the working set)"; report.Retried != want {
		t.Fatalf("expected the retry notice %q, got %q", want, report.Retried)
	}
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
	if err := gateway.ClearNote(context.Background(), ""); err == nil {
		t.Fatal("expected clearing a note with no key to be refused")
	}
}

// A bd that could not resolve the remote's name says so in the sync's own words,
// and comes back as a name that could not be resolved, so that a dispatch can
// wait the network out. It is still bd's halt underneath, with the exit code bd
// gave, so a sync run by hand reads the same number as before.
func TestSyncNamesAFailureToResolveAHostAndKeepsBdsExitCode(t *testing.T) {
	gateway := standIn(t, "fatal: unable to access 'https://x/': Could not resolve host: github.com", 1)

	err := gateway.Sync(context.Background())
	unresolved, ok := application.Unresolved(err)
	if !ok {
		t.Fatalf("expected a name that could not be resolved, got %T: %v", err, err)
	}
	if !strings.Contains(unresolved.Said, "Could not resolve host: github.com") {
		t.Fatalf("expected the line git said, got %q", unresolved.Said)
	}
	halt, ok := application.Halted(err)
	if !ok || halt.Code != 1 {
		t.Fatalf("expected the halt bd gave to be kept underneath, got %v", err)
	}
}

func TestSyncDoesNotCallAnyOtherFailureAnUnresolvedName(t *testing.T) {
	err := standIn(t, "CONFLICT in the working set", 2).Sync(context.Background())
	if _, ok := application.Unresolved(err); ok {
		t.Fatalf("expected a conflict not to be a name that could not be resolved, got %v", err)
	}
}
