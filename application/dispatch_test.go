package application_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// features/dispatch.feature covers dispatching against a real git rig; these
// are the corners around it — what a dispatch refuses, what it passes over, and
// what it leaves behind when a step fails — walked through with stand-ins for
// everything, including git.

// fakeWorktrees is an in-memory application.Worktrees: nothing is cut, and the
// test says which step fails.
type fakeWorktrees struct {
	mu      sync.Mutex
	fetched []string
	added   []string
	removed []string

	FetchErr, AddErr, RemoveErr error
}

var _ application.Worktrees = (*fakeWorktrees)(nil)

func (f *fakeWorktrees) Fetch(_ context.Context, rigDir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, rigDir)
	return f.FetchErr
}

func (f *fakeWorktrees) Add(_ context.Context, _, dir, branch, start string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AddErr != nil {
		return f.AddErr
	}
	f.added = append(f.added, fmt.Sprintf("%s %s %s", dir, branch, start))
	return nil
}

func (f *fakeWorktrees) Remove(_ context.Context, _, dir, branch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, dir+" "+branch)
	return f.RemoveErr
}

func (f *fakeWorktrees) was() (added, removed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.added...), append([]string(nil), f.removed...)
}

// aFactory is a dispatch with everything stood in for: a vault on disk holding
// the Builder's charter, a fake tracker holding one epic, and fakes for the
// worktrees and the runner.
func aFactory(t *testing.T) (application.Dispatch, *apptest.FakeTracker, *fakeWorktrees, *apptest.FakeRunner, string) {
	t.Helper()

	dir := t.TempDir()
	charter := filepath.Join(dir, vault.SeatsDir, "builder", vault.CharterFile)
	if err := os.MkdirAll(filepath.Dir(charter), 0o755); err != nil {
		t.Fatalf("making the vault: %v", err)
	}
	if err := os.WriteFile(charter, []byte("# Builder — charter\n\nYou work one story.\n"), 0o644); err != nil {
		t.Fatalf("writing the charter: %v", err)
	}

	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
	})
	worktrees, runner := &fakeWorktrees{}, apptest.NewFakeRunner()

	return application.Dispatch{
		Tracker:   tracker,
		Worktrees: worktrees,
		Runner:    runner,
		Boot: application.SeatBoot{
			Vault:   vault.New(dir),
			Harness: claude.New(),
			Seat:    "builder",
			Host:    "vps",
		},
		Host: "vps",
		Cap:  1,
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
	}, tracker, worktrees, runner, dir
}

func TestDispatchRefusesWhatItCannotDo(t *testing.T) {
	ctx := context.Background()
	whole, _, _, _, _ := aFactory(t)

	for _, refused := range []struct {
		why      string
		dispatch application.Dispatch
		says     string
	}{
		{"no tracker", application.Dispatch{Host: "vps", Cap: 1}, "work tracker"},
		{"no host", application.Dispatch{
			Tracker: whole.Tracker, Worktrees: whole.Worktrees, Runner: whole.Runner, Cap: 1,
		}, "MW_HOST"},
		{"a cap of nothing", application.Dispatch{
			Tracker: whole.Tracker, Worktrees: whole.Worktrees, Runner: whole.Runner, Host: "vps",
		}, "cap"},
	} {
		if _, err := refused.dispatch.Run(ctx); err == nil {
			t.Errorf("expected a dispatch with %s to be refused", refused.why)
		} else if !strings.Contains(err.Error(), refused.says) {
			t.Errorf("expected the refusal of %s to say %q, got %q", refused.why, refused.says, err)
		}
	}
}

func TestDispatchPassesOverAStoryWhoseRigIsNotCheckedOutHere(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.Rigs = map[string]string{"fellowship": "/rigs/fellowship"}
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected nothing started, got %+v", report.Started)
	}
	if len(report.Passed) != 1 || !strings.Contains(report.Passed[0].Why, "millwright") {
		t.Fatalf("expected the story passed over for want of its rig, got %+v", report.Passed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Assignee != "" {
		t.Fatalf("expected an untouched story to be unclaimed, got %q", detail.Assignee)
	}
}

func TestDispatchBootsAStoryWhoseFormulaIsNotInstalledWithoutPouringIt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 || len(runner.Names()) != 1 {
		t.Fatalf("expected the story to be started anyway, got %+v", report)
	}
	if tracker.Molecules() != 0 {
		t.Fatalf("expected nothing to have been poured, got %d molecules", tracker.Molecules())
	}

	booted, err := os.ReadFile(filepath.Join(vaultDir, vault.RunsDir, "mw-gq6.1", application.BootFileName))
	if err != nil {
		t.Fatalf("reading the boot file: %v", err)
	}
	if !strings.Contains(string(booted), "not installed") {
		t.Fatalf("expected the boot file to say the formula was not poured, got %q", booted)
	}
}

func TestDispatchGivesBackTheClaimAndTheWorktreeWhenTheBootFails(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	// A seat that is not in the vault: the boot cannot be assembled.
	dispatch.Boot.Seat = "nobody"

	report, err := dispatch.Run(ctx)
	if err == nil {
		t.Fatal("expected the dispatch to report the failure")
	}
	if len(report.Failed) != 1 || !report.Failed[0].Released {
		t.Fatalf("expected one failure with the claim given back, got %+v", report.Failed)
	}
	if len(runner.Names()) != 0 {
		t.Fatalf("expected no session, got %q", runner.Names())
	}

	added, removed := worktrees.was()
	if len(added) != 1 || len(removed) != 1 {
		t.Fatalf("expected the worktree cut and then removed, got added %q removed %q", added, removed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Assignee != "" || detail.Status == apptest.StatusInProgress {
		t.Fatalf("expected the claim to be given back, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	if comments := tracker.Comments("mw-gq6.1"); len(comments) != 1 {
		t.Fatalf("expected one comment saying why, got %q", comments)
	}
}

func TestDispatchKeepsTheClaimWhenTheSessionIsAlreadyRunning(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "Understand", Description: "Read it."})

	// The story is dispatched, and only the recording of it fails. A session
	// that is spending fuel is never given back.
	dispatch.Tracker = &stateRefusing{FakeTracker: tracker}

	report, err := dispatch.Run(ctx)
	if err == nil {
		t.Fatal("expected the dispatch to report that the run state was not recorded")
	}
	if len(report.Started) != 1 || len(runner.Names()) != 1 {
		t.Fatalf("expected the session to be running, got %+v", report)
	}
	if len(report.Failed) != 1 || report.Failed[0].Released {
		t.Fatalf("expected the failure to leave the claim alone, got %+v", report.Failed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the story to stay claimed under a live session, got %q", detail.Status)
	}
	if molecule, poured := tracker.Poured("mw-gq6.1"); !poured || !molecule.Poured() {
		t.Fatalf("expected the formula to have been poured, got %+v", molecule)
	}
}

// startOldSession leaves a session under the story's name on the runner, as an
// earlier run did, and returns the name.
func startOldSession(t *testing.T, runner *apptest.FakeRunner, id string) string {
	t.Helper()
	name := application.SessionName(id)
	if err := runner.Start(context.Background(), application.SessionSpec{Name: name, Dir: "/old", Command: []string{"claude"}}); err != nil {
		t.Fatalf("starting the earlier session: %v", err)
	}
	return name
}

func TestDispatchClearsAnEndedSessionOfTheSameNameWhateverItsExitWas(t *testing.T) {
	for name, end := range map[string]func(*apptest.FakeRunner, string){
		"exited":         func(r *apptest.FakeRunner, n string) { r.Exit(n, 1) },
		"status unknown": func(r *apptest.FakeRunner, n string) { r.ExitUnknown(n) },
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, worktrees, runner, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
			session := startOldSession(t, runner, "mw-gq6.1")
			end(runner, session)

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("expected the story to be dispatched again, got %v", err)
			}
			if len(report.Started) != 1 || len(runner.Names()) != 1 {
				t.Fatalf("expected one session, got %+v", report)
			}
			if closed := runner.Closed(); len(closed) != 1 || closed[0] != session {
				t.Errorf("expected the dead %s to be closed, got %q", session, closed)
			}
			if status, _ := runner.Status(context.Background(), session); !status.Running() {
				t.Errorf("expected the new session to be running, got %+v", status)
			}
			if _, removed := worktrees.was(); len(removed) != 0 {
				t.Errorf("expected the worktree to be kept, got %q removed", removed)
			}
		})
	}
}

func TestDispatchRefusesAStoryWhoseSessionIsStillRunningAndTouchesNothing(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	session := startOldSession(t, runner, "mw-gq6.1")

	report, err := dispatch.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("expected the dispatch to refuse a story whose session is running, got %v", err)
	}
	if len(report.Started) != 0 || len(report.Failed) != 1 {
		t.Fatalf("expected one failure and nothing started, got %+v", report)
	}
	if closed := runner.Closed(); len(closed) != 0 {
		t.Errorf("expected nothing to be closed, got %q", closed)
	}
	if status, _ := runner.Status(ctx, session); !status.Running() {
		t.Errorf("expected the running session to be left alone, got %+v", status)
	}
	if added, removed := worktrees.was(); len(added)+len(removed) != 0 {
		t.Errorf("expected no worktree to be cut or removed, got %q added and %q removed", added, removed)
	}
	detail, _ := tracker.ShowStory(ctx, "mw-gq6.1")
	if detail.Assignee != "" || detail.Status == apptest.StatusInProgress {
		t.Errorf("expected the story to be left unclaimed, got status %q assignee %q", detail.Status, detail.Assignee)
	}
}

func TestDispatchGivesBackTheClaimAndTheWorktreeWhenTheDeadSessionWillNotClose(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	runner.Exit(startOldSession(t, runner, "mw-gq6.1"), 1)
	dispatch.Runner = &closeRefusing{FakeRunner: runner}

	report, err := dispatch.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "clearing the dead session") {
		t.Fatalf("expected the dispatch to say the dead session could not be cleared, got %v", err)
	}
	if len(report.Failed) != 1 || !report.Failed[0].Released {
		t.Fatalf("expected the claim to be given back, got %+v", report.Failed)
	}
	if _, removed := worktrees.was(); len(removed) != 1 {
		t.Errorf("expected the worktree this dispatch cut to be removed, got %q", removed)
	}
}

// closeRefusing is a runner that cannot close a session.
type closeRefusing struct {
	*apptest.FakeRunner
}

func (r *closeRefusing) Close(context.Context, string) error {
	return fmt.Errorf("the runner would not close it")
}

// stateRefusing is a tracker that does everything but record a story's state.
type stateRefusing struct {
	*apptest.FakeTracker
}

func (t *stateRefusing) SetStoryState(context.Context, string, string, string, string) error {
	return fmt.Errorf("the tracker would not write the state")
}
