package application_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

	// OnAdd, when set, runs before Add returns AddErr — the hook a test uses to
	// stand in for a second dispatcher winning the race for the same story in
	// the instant between this one's Add failing and its release running.
	OnAdd func()
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
		if f.OnAdd != nil {
			f.OnAdd()
		}
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

func (f *fakeWorktrees) RemoveWithoutForce(_ context.Context, _, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, dir)
	return f.RemoveErr
}

func (f *fakeWorktrees) DeleteBranch(_ context.Context, _, _ string) error {
	return nil
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
	// Installed with no steps: what most of this file's tests care about is
	// everything past the claim, not the formula. The tests about the formula
	// itself install it with their own steps, or leave it out on purpose.
	tracker.AddFormula("tdd-feature")
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

// mw-gq6.95: a story whose path names a formula this vault has not installed
// is refused before it is claimed, rather than started anyway with nothing
// poured for it — the boot file mw-gq6.94 left behind, with no step beads and
// nothing for the Builder to follow but the rig's own docs.
func TestDispatchDoesNotClaimAStoryWhoseFormulaIsNotInstalled(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{
		ID: "mw-gq6.1", Title: "A story",
		Overrides: domain.Path{Formula: "story"},
	})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected nothing to be started, got %+v", report)
	}
	if len(report.Passed) != 1 || !strings.Contains(report.Passed[0].Why, "formula story is not installed") {
		t.Fatalf("expected the story to be passed over for its formula, got %+v", report.Passed)
	}
	if status, err := tracker.ShowStory(ctx, "mw-gq6.1"); err != nil || status.Status != application.StatusOpen {
		t.Fatalf("expected the story to stay unclaimed, got %+v (%v)", status, err)
	}
	if got := tracker.Comments("mw-gq6.1"); len(got) != 1 || !strings.Contains(got[0], application.ReasonFormulaNotInstalled) {
		t.Fatalf("expected exactly one comment naming the reason, got %q", got)
	}

	// A second dispatch says nothing more.
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching again: %v", err)
	}
	if got := tracker.Comments("mw-gq6.1"); len(got) != 1 {
		t.Fatalf("expected the comment not to be repeated, got %q", got)
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

func TestDispatchWorksAnOpenMoleculeAgainAndDoesNotCallItLeftBehindWhenTheBootFails(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"}, application.FormulaStep{Title: "Two"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	first, err := tracker.PourFormula(ctx, "tdd-feature", "mw-gq6.1", "A story")
	if err != nil {
		t.Fatalf("pouring: %v", err)
	}
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.MoleculeField: first.RootID}); err != nil {
		t.Fatalf("recording the molecule: %v", err)
	}
	dispatch.Boot.Seat = "nobody"

	report, err := dispatch.Run(ctx)
	if err == nil {
		t.Fatal("expected the dispatch to report the failure")
	}
	if tracker.Molecules() != 1 {
		t.Fatalf("expected the open molecule to be reused, got %d molecules", tracker.Molecules())
	}
	if strings.Contains(err.Error(), "left behind") || strings.Contains(report.Failed[0].Err.Error(), "poured as") {
		t.Fatalf("expected the failure not to say a molecule was poured and left behind, got %q", err)
	}
}

func TestDispatchGivesBackTheClaimWhenTheTrackerCannotSayIfTheMoleculeIsOpen(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.MoleculeField: "f-mol-1"}); err != nil {
		t.Fatalf("recording the molecule: %v", err)
	}
	tracker.FailOn("OpenMolecule", fmt.Errorf("the database is locked"))

	report, err := dispatch.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "the database is locked") {
		t.Fatalf("expected the dispatch to say why, got %v", err)
	}
	if len(report.Failed) != 1 || !report.Failed[0].Released {
		t.Fatalf("expected one failure with the claim given back, got %+v", report.Failed)
	}
	if tracker.Molecules() != 0 {
		t.Fatalf("expected no second molecule poured on a guess, got %d", tracker.Molecules())
	}
	if len(runner.Names()) != 0 {
		t.Fatalf("expected no session, got %q", runner.Names())
	}
	if _, removed := worktrees.was(); len(removed) != 1 {
		t.Fatalf("expected the worktree removed, got %q", removed)
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

// mw-gq6.96: two dispatchers can pass the namesake check and claim the same
// story in the same instant — the check at the top of start is not atomic
// with the claim — and then race to cut its worktree. The loser here fails
// cutting the worktree with the error git gives when the branch already
// exists, because the winner has already cut it and, by the time the loser
// gets to release, started its session too. The loser must not clear a claim
// the winner is now working under.
func TestDispatchLeavesTheClaimWhenAnotherDispatcherStartedTheStoryInTheSameInstant(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	worktrees.AddErr = fmt.Errorf("fatal: a branch named 'mw/mw-gq6.1' already exists")
	session := application.SessionName("mw-gq6.1")
	worktrees.OnAdd = func() {
		if err := runner.Start(ctx, application.SessionSpec{Name: session, Dir: "/other", Command: []string{"claude"}}); err != nil {
			t.Fatalf("starting the other dispatcher's session: %v", err)
		}
	}

	report, err := dispatch.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), session) {
		t.Fatalf("expected the failure to name the running session %s, got %v", session, err)
	}
	if len(report.Failed) != 1 || report.Failed[0].Released {
		t.Fatalf("expected one failure that left the claim alone, got %+v", report.Failed)
	}
	for _, asked := range tracker.Asked() {
		if asked == "ReleaseClaim" {
			t.Fatalf("expected the claim not to be released, got %q", tracker.Asked())
		}
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the story to stay claimed under the running session, got %q", detail.Status)
	}
	if comments := tracker.Comments("mw-gq6.1"); len(comments) != 1 || !strings.Contains(comments[0], session) {
		t.Fatalf("expected one comment naming the running session, got %q", comments)
	}
}

// mw-gq6.96: the same worktree failure, but with no session of the story's
// name running anywhere — the ordinary case, where this dispatcher really is
// the only one, and the claim it took is given back exactly as before.
func TestDispatchStillGivesBackTheClaimWhenTheWorktreeFailsAndNoSessionIsRunning(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	worktrees.AddErr = fmt.Errorf("fatal: a branch named 'mw/mw-gq6.1' already exists")

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

func TestDispatchMarksASyncHaltAndLeavesNothingClaimed(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	sync := &stubSync{err: &application.SyncHalt{Code: 2, Said: "conflict in the working set"}}
	marker := apptest.NewFakeSyncHaltMarker()
	dispatch.Sync = sync
	dispatch.SyncHalts = marker

	if _, err := dispatch.Run(ctx); err == nil {
		t.Fatal("expected a halted sync to stop the dispatch")
	}
	info, there, err := marker.Read(ctx)
	if err != nil || !there {
		t.Fatalf("expected the marker written, there=%v, err=%v", there, err)
	}
	if info.Said != "conflict in the working set" {
		t.Fatalf("expected the marker to hold what bd said, got %+v", info)
	}
}

func TestDispatchClearsTheMarkerOnceLevelAgain(t *testing.T) {
	ctx := context.Background()
	dispatch, _, _, _, _ := aFactory(t)
	sync := &stubSync{}
	marker := apptest.NewFakeSyncHaltMarker()
	if err := marker.Write(ctx, application.SyncHaltInfo{At: time.Now().Add(-time.Hour), Said: "old"}); err != nil {
		t.Fatalf("seeding the marker: %v", err)
	}
	dispatch.Sync = sync
	dispatch.SyncHalts = marker

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("expected a level sync with nothing ready to succeed, got %v", err)
	}
	if _, there, _ := marker.Read(ctx); there {
		t.Fatal("expected a level sync to clear the marker")
	}
}

// mw-gq6.86: a story's second attempt starts with a blank pane too, the same
// one mw sweep may have fingerprinted for the first attempt before it ended.
// Left in place, that note's clock survives into the second attempt, and a
// sweep right after it starts reads it as this attempt's own silence, since
// the first attempt's clock — which is well past the stale threshold by then.
func TestDispatchClearsTheSweepNoteSoAFreshAttemptIsNotMarkedStuckFromTheLastOnesClock(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Memory = tracker

	clock := time.Date(2026, 9, 22, 0, 50, 0, 0, time.UTC)
	sweep := application.Sweep{
		Tracker: tracker, Runner: runner, Memory: tracker, Host: "vps",
		Now: func() time.Time { return clock },
	}

	// Attempt 1 starts, its pane blank, and a sweep right after remembers it.
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching attempt 1: %v", err)
	}
	if _, err := sweep.Run(ctx); err != nil {
		t.Fatalf("sweeping after attempt 1 started: %v", err)
	}

	// Attempt 1's session ends before it prints anything, and the story is
	// given back the way mw next leaves it once a session is gone.
	runner.Exit(application.SessionName("mw-gq6.1"), 1)
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatalf("giving back the claim: %v", err)
	}

	// Attempt 2 is dispatched, well past the stale threshold since attempt 1's
	// note was written.
	clock = clock.Add(2*time.Hour + time.Minute)
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching attempt 2: %v", err)
	}

	report, err := sweep.Run(ctx)
	if err != nil {
		t.Fatalf("sweeping right after attempt 2 started: %v", err)
	}
	if got := tracker.State("mw-gq6.1", application.RunState); got == application.RunStuck {
		t.Fatalf("expected the fresh attempt not to be recorded %s=%s, got %q", application.RunState, application.RunStuck, got)
	}
	for _, d := range report.Stuck {
		if d.Story.ID == "mw-gq6.1" {
			t.Fatalf("expected mw-gq6.1 not to be reported stuck, got %+v", report.Stuck)
		}
	}
}
