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

// TestASyncStaysLevelWhenGCsRepackOfTheRemoteCacheFails is the rest of this
// story's third acceptance criterion, through application.Sync and the real
// gateway together: a repack failure must never be the reason a sync that
// otherwise got level comes back as one that did not.
func TestASyncStaysLevelWhenGCsRepackOfTheRemoteCacheFails(t *testing.T) {
	gateway, _ := recordingStandIn(t)
	broken := filepath.Join(gateway.Vault(), ".beads", "embeddeddolt", "x", ".dolt", "git-remote-cache", "h", "repo.git")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatalf("making a broken remote cache: %v", err)
	}
	sync := application.Sync{
		Vault:   &apptest.FakeVaultFiles{},
		Tracker: gateway,
		Host:    "vps",
	}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a failed repack not to fail the sync, got %v", err)
	}
	if report.GCed {
		t.Fatal("expected the failed repack to be reported like any other GC failure, not as a collection that happened")
	}
	if report.At.IsZero() {
		t.Fatal("expected the sync to still record the host as level")
	}
}

// recordingStandIn writes a program that always exits 0 but first writes the
// arguments it was called with, space-joined, on their own line in a file
// beside it — so a test can say exactly what a gateway method ran, not just
// that it exited without error.
func recordingStandIn(t *testing.T) (gateway *beads.Gateway, calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	path := filepath.Join(dir, "bd-stand-in")
	script := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\nexit 0\n", calls)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, beads.WithProgram(path)), calls
}

func TestGCSkipsDecayAndAsksBdToForceItThroughWithoutAPrompt(t *testing.T) {
	gateway, calls := recordingStandIn(t)

	if err := gateway.GC(context.Background()); err != nil {
		t.Fatalf("collecting: %v", err)
	}
	said, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("reading what was called: %v", err)
	}
	line := strings.TrimSpace(string(said))
	if !strings.Contains(line, "gc") || !strings.Contains(line, "--skip-decay") || !strings.Contains(line, "--force") {
		t.Fatalf("expected gc to skip decay and force past the prompt, got %q", line)
	}
}

// TestGCReportsARepackFailureLikeAnyOtherGCFailure covers the story's third
// acceptance criterion at the gateway: a remote cache repack that fails —
// here, one that is not really a git repository — is reported the same way a
// failed `bd gc` itself would be, as a plain error from GC, not a panic and
// not a silent success.
func TestGCReportsARepackFailureLikeAnyOtherGCFailure(t *testing.T) {
	gateway, _ := recordingStandIn(t)
	broken := filepath.Join(gateway.Vault(), ".beads", "embeddeddolt", "x", ".dolt", "git-remote-cache", "h", "repo.git")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatalf("making a broken remote cache: %v", err)
	}

	if err := gateway.GC(context.Background()); err == nil {
		t.Fatal("expected a remote cache that is not a real repository to fail the repack")
	}
}

func TestSizeSumsEveryFileUnderBeadsAndIgnoresTheRest(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(filepath.Join(beadsDir, "backup"), 0o755); err != nil {
		t.Fatalf("making the beads directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), make([]byte, 100), 0o644); err != nil {
		t.Fatalf("writing a database file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "backup", "chunk.darc"), make([]byte, 250), 0o644); err != nil {
		t.Fatalf("writing a backup chunk: %v", err)
	}
	// A file outside .beads must never be counted: it is not this database's.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), make([]byte, 9999), 0o644); err != nil {
		t.Fatalf("writing an unrelated file: %v", err)
	}

	gateway := beads.New(dir)
	size, err := gateway.Size(context.Background())
	if err != nil {
		t.Fatalf("sizing: %v", err)
	}
	if size != 350 {
		t.Fatalf("expected 100+250=350 bytes under .beads, got %d", size)
	}
}

func TestSizeOfAVaultWithNoBeadsDirectoryYetIsZeroNotAFailure(t *testing.T) {
	gateway := beads.New(t.TempDir())
	size, err := gateway.Size(context.Background())
	if err != nil {
		t.Fatalf("expected no .beads yet to size as zero, got %v", err)
	}
	if size != 0 {
		t.Fatalf("expected 0, got %d", size)
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
