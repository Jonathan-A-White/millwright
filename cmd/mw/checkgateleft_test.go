package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aBranchOneAhead is a landing whose story branch holds one commit and whose
// worktree holds nothing uncommitted: all a whole check reads of it before the
// rig's tests.
type aBranchOneAhead struct{ application.Landing }

func (aBranchOneAhead) Ahead(context.Context, string, string, string) (int, error) { return 1, nil }
func (aBranchOneAhead) Uncommitted(context.Context, string) ([]string, error)      { return nil, nil }
func (aBranchOneAhead) Commits(context.Context, string, string, string) ([]application.Commit, error) {
	return nil, nil
}
func (aBranchOneAhead) RefHead(context.Context, string, string) (string, error) { return "", nil }

// testsThatMustNotRun are the rig's tests, which a check that could not take
// the gate lock must not start.
type testsThatMustNotRun struct{ ran int }

func (r *testsThatMustNotRun) Run(context.Context, string, string) (application.Checked, error) {
	r.ran++
	return application.Checked{Command: "make test", Passed: true}, nil
}

// aRunRecord is the vault's run files, in memory.
type aRunRecord struct {
	mu    sync.Mutex
	files map[string]string
}

func (r *aRunRecord) PutRunFile(_ context.Context, story, name, contents string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files == nil {
		r.files = map[string]string{}
	}
	r.files[story+"/"+name] = contents
	return name, nil
}

func (r *aRunRecord) ReadRunFile(_ context.Context, story, name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if text, ok := r.files[story+"/"+name]; ok {
		return text, nil
	}
	return "", os.ErrNotExist
}

func (r *aRunRecord) RunFile(story, name string) string {
	return "/vault/runs/" + story + "/" + name
}

// aCheckBehindAHeldGateLock is mw check on a story whose formula was poured, with
// the host's gate lock held by another story's close-out.
func aCheckBehindAHeldGateLock(t *testing.T, handBackAtOnce bool, keep time.Duration) (string, error, *testsThatMustNotRun) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	tracker.AddFormula("tdd-feature",
		application.FormulaStep{ID: "s-1", Title: "Write the failing test first"},
		application.FormulaStep{ID: "s-2", Title: "Run the full build, tests and vet"})
	poured, err := tracker.PourFormula(context.Background(), "tdd-feature", "mw-x.1", "One", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}); err != nil {
		t.Fatal(err)
	}
	gate := hostlock.NewGate(t.TempDir(), hostlock.WithGatePoll(5*time.Millisecond))
	holder, err := gate.Take(context.Background(), "mw-other.9", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { holder.Holding.Release(context.Background()) })
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := &testsThatMustNotRun{}
	records := &aRunRecord{}
	var said strings.Builder
	_, err = application.Check{
		Tracker: tracker, Landing: aBranchOneAhead{}, Checks: tests,
		GateLock: gate, Slot: rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond)), Records: records, Logs: records,
		PartSlotWait: 150 * time.Millisecond, HandBackAtOnce: handBackAtOnce, KeepPlace: keep,
		Vault: "/home/mw/vault", Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": rigDir},
		Out:  &said,
	}.Run(context.Background(), "mw-x.1")
	return said.String(), err, tests
}

// A whole check that reaches its wait limit with the rig's tests not run names
// the formula step of the gate by its id and says in one line what to do: close
// it saying the gate is left to the close-out, close the steps the Builder's own
// runs prove, write the closing comment and end the turn (mw-gq6.1557).
func TestCheckWaitLimitLeavesTheGateToTheCloseOut(t *testing.T) {
	said, err, tests := aCheckBehindAHeldGateLock(t, true, 0)
	if err == nil {
		t.Fatalf("expected the exit code of a check that ran nothing, got none:\n%s", said)
	}
	if tests.ran != 0 {
		t.Fatalf("expected the rig's tests not to run, they ran %d time(s)", tests.ran)
	}
	var line string
	for _, l := range strings.Split(said, "\n") {
		if strings.Contains(l, "wait limit") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("expected a line naming the wait limit, got:\n%s", said)
	}
	for _, want := range []string{
		"f-mol-1.2", "Run the full build, tests and vet",
		`bd -C /home/mw/vault close`, `--reason "gate left to the close-out: /vault/runs/mw-x.1/check-1.log"`,
		"write the closing comment", "end the turn", "mw next runs the gate",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to say %q, got:\n%s", want, line)
		}
	}
	if strings.Contains(line, "f-mol-1.1") {
		t.Errorf("expected only the gate's step to be named, got:\n%s", line)
	}
	if strings.Contains(line, "hand back at once") {
		t.Errorf("expected no word to hand back at once with steps open, got:\n%s", line)
	}
}

// A foreground check whose wait runs out with its place kept still says to run
// the same command again, and leaves the gate to no one.
func TestCheckWaitLimitLeavesTheGateToTheCloseOutOnlyWithNoPlaceKept(t *testing.T) {
	said, _, _ := aCheckBehindAHeldGateLock(t, false, application.CheckKeepPlace)
	if !strings.Contains(said, "kept for 10 minutes") || !strings.Contains(said, "run the same command again") {
		t.Errorf("expected the run-it-again line, got:\n%s", said)
	}
	if strings.Contains(said, "gate left to the close-out") {
		t.Errorf("expected no word of leaving the gate to the close-out, got:\n%s", said)
	}
}
