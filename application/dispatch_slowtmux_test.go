package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.886: every tmux command runs under a 5 s deadline (mw-gq6.884), so on a
// loaded host a look at a session that fails is an ordinary event. A session that
// could not be asked about is not known to be gone: nothing is removed,
// committed, given back or dropped for it.

// slowTmuxRunner is a runner whose Status alone can fail the way a loaded tmux
// does, and whose Start can fail too; FakeRunner.Err fails every method.
type slowTmuxRunner struct {
	*apptest.FakeRunner
	mu       sync.Mutex
	slow     bool
	startErr error
}

func (s *slowTmuxRunner) setSlow(slow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slow = slow
}

func (s *slowTmuxRunner) Status(ctx context.Context, name string) (application.SessionStatus, error) {
	s.mu.Lock()
	slow := s.slow
	s.mu.Unlock()
	if slow {
		return application.SessionStatus{}, errors.New("tmux list-panes -t =" + name + ": did not finish in 5s and was stopped: context deadline exceeded")
	}
	return s.FakeRunner.Status(ctx, name)
}

func (s *slowTmuxRunner) Start(ctx context.Context, spec application.SessionSpec) error {
	s.mu.Lock()
	err := s.startErr
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.FakeRunner.Start(ctx, spec)
}

// aSlowTmuxFactory is aFactory with a runner whose Status always fails, and a
// clock the test moves.
func aSlowTmuxFactory(t *testing.T) (application.Dispatch, *apptest.FakeTracker, *fakeWorktrees, *slowTmuxRunner, *time.Time) {
	t.Helper()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	slow := &slowTmuxRunner{FakeRunner: runner, slow: true}
	dispatch.Runner = slow
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	return dispatch, tracker, worktrees, slow, &now
}

func asked(tracker *apptest.FakeTracker, call string) bool {
	return slices.Contains(tracker.Asked(), call)
}

// A start that failed with tmux too slow to say whether a session of the story's
// name is running removes no worktree or branch and gives no claim back, and
// says so on the story; the next tick, with tmux answering, settles the claim.
func TestAStartThatFailsWithTmuxTooSlowToAnswerKeepsTheWorktreeAndTheClaim(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fail     func(*fakeWorktrees, *slowTmuxRunner)
		worktree bool // the failure comes after the worktree was cut
	}{
		{"the session start failed", func(_ *fakeWorktrees, r *slowTmuxRunner) { r.startErr = errors.New("tmux: duplicate session") }, true},
		{"the worktree could not be cut", func(w *fakeWorktrees, _ *slowTmuxRunner) { w.AddErr = errors.New("git: cannot lock ref") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dispatch, tracker, worktrees, runner, now := aSlowTmuxFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
			tc.fail(worktrees, runner)

			report, err := dispatch.Run(ctx)
			if err == nil || !strings.Contains(err.Error(), "could not be asked about") {
				t.Fatalf("expected the failure to say the session could not be asked about, got %v", err)
			}
			if len(report.Failed) != 1 || report.Failed[0].Released {
				t.Fatalf("expected one failure that kept the claim, got %+v", report.Failed)
			}
			if _, removed := worktrees.was(); len(removed) != 0 {
				t.Errorf("expected no worktree or branch removed for a session not known to be gone, got %q", removed)
			}
			if tc.worktree && !strings.Contains(err.Error(), "no worktree or branch was removed") {
				t.Errorf("expected the failure to say no worktree or branch was removed, got %v", err)
			}
			if asked(tracker, "ReleaseClaim") {
				t.Errorf("expected the claim not to be given back, got %q", tracker.Asked())
			}
			detail, serr := tracker.ShowStory(ctx, "mw-gq6.1")
			if serr != nil || detail.Status != apptest.StatusInProgress {
				t.Fatalf("expected the story to stay claimed, got %q, %v", detail.Status, serr)
			}
			if comments := tracker.Comments("mw-gq6.1"); len(comments) != 1 || !strings.Contains(comments[0], "kept") {
				t.Errorf("expected one comment saying the claim was kept, got %q", comments)
			}

			// Later: nothing is running, the lease has run out, and tmux answers.
			runner.setSlow(false)
			runner.startErr = nil
			worktrees.AddErr = nil
			*now = now.Add(apptest.LeaseTTL + time.Minute)
			again, err := dispatch.Run(ctx)
			if err != nil {
				t.Fatalf("the next tick: %v", err)
			}
			if len(again.Reclaimed) != 1 || again.Reclaimed[0].StoryID != "mw-gq6.1" {
				t.Fatalf("expected the next tick to give the kept claim back, got %+v", again.Reclaimed)
			}
		})
	}
}

// An earlier attempt's worktree is not cleared away on a tick on which the
// story's session could not be asked about: the story is passed over, and the
// claim is kept, since it is not known that no session is running.
func TestALeftoverIsNotClearedAwayWhileTmuxIsTooSlowToAnswer(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, _, _ := aSlowTmuxFactory(t)
	claimedStory(t, tracker, "mw-gq6.1", 1)
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	worktrees.Leave(application.WorktreeDir("/rigs/millwright", "mw-gq6.1"), application.StoryBranch("mw-gq6.1"))
	landing := &fakeRetryLanding{UncommittedPaths: []string{"half-done.go"}}
	dispatch.Landing = landing

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the run not to fail for one story passed over, got %v", err)
	}
	if len(report.Passed) != 1 || report.Passed[0].Gate != application.LeftoverRefusedGate || !strings.Contains(report.Passed[0].Why, "could not be asked about") {
		t.Fatalf("expected the story passed over, its session not asked about, got passed %+v failed %+v", report.Passed, report.Failed)
	}
	if len(landing.leftoverMessages) != 0 {
		t.Errorf("expected nothing committed, got %q", landing.leftoverMessages)
	}
	if got := worktrees.renames(); len(got) != 0 {
		t.Errorf("expected nothing renamed, got %q", got)
	}
	if added, removed := worktrees.was(); len(added) != 0 || len(removed) != 0 {
		t.Errorf("expected nothing cut or removed, got %q %q", added, removed)
	}
	if detail, serr := tracker.ShowStory(ctx, "mw-gq6.1"); serr != nil || detail.Status != apptest.StatusInProgress {
		t.Errorf("expected the claim kept, as it is not known that no session is running, got %q, %v", detail.Status, serr)
	}
}

// Uncommitted files in a worktree whose session could not be asked about, a
// moment after it could be, are not committed as an earlier attempt's leftovers.
func TestUncommittedFilesAreNotCommittedAsLeftoversWhileTmuxIsTooSlowToAnswer(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aSlowTmuxFactory(t)
	runner.setSlow(false)
	claimedStory(t, tracker, "mw-gq6.1", 1)
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	worktrees.Leave(application.WorktreeDir("/rigs/millwright", "mw-gq6.1"), application.StoryBranch("mw-gq6.1"))
	landing := &fakeRetryLanding{UncommittedPaths: []string{"half-done.go"}}
	dispatch.Landing = landing
	// tmux answers when the leftover is first looked for, and is slow by the time
	// the worktree's files are read.
	var looked int
	worktrees.OnExists = func() {
		looked++
		if looked == 2 {
			runner.setSlow(true)
		}
	}

	report, _ := dispatch.Run(ctx)
	if len(report.Passed) != 1 || report.Passed[0].Gate != application.LeftoverRefusedGate || !strings.Contains(report.Passed[0].Why, "could not be asked about") {
		t.Fatalf("expected the story passed over, its session not asked about, got passed %+v failed %+v", report.Passed, report.Failed)
	}
	if len(landing.leftoverMessages) != 0 {
		t.Errorf("expected nothing committed, got %q", landing.leftoverMessages)
	}
	if got := worktrees.renames(); len(got) != 0 {
		t.Errorf("expected nothing renamed, got %q", got)
	}
	if added, removed := worktrees.was(); len(added) != 0 || len(removed) != 0 {
		t.Errorf("expected nothing cut or removed, got %q %q", added, removed)
	}
}

// A story the Mayor held after it was claimed keeps its claim, and the hold is
// not written over, while its session could not be asked about.
func TestAHeldStoryKeepsItsClaimWhileTmuxIsTooSlowToAnswer(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, _, _ := aSlowTmuxFactory(t)
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Tracker = &heldTracker{FakeTracker: tracker, afterClaim: true}

	report, err := dispatch.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "could not be asked about") {
		t.Fatalf("expected the failure to say the session could not be asked about, got %v", err)
	}
	if len(report.Failed) != 1 || report.Failed[0].Released {
		t.Fatalf("expected one failure that kept the claim, got %+v", report.Failed)
	}
	if asked(tracker, "ReleaseClaim") || asked(tracker, "HoldClaimHeldBy") {
		t.Errorf("expected the claim kept and the hold not written over, got %q", tracker.Asked())
	}
	if _, removed := worktrees.was(); len(removed) != 0 {
		t.Errorf("expected nothing removed, got %q", removed)
	}
	detail, serr := tracker.ShowStory(ctx, "mw-gq6.1")
	if serr != nil || detail.Assignee == "" || !detail.Held() {
		t.Fatalf("expected the story held and still claimed, got status %q assignee %q, %v", detail.Status, detail.Assignee, serr)
	}
	// No later tick settles it: a held story is on no host's list of what runs,
	// so the story says the claim is to be given back by hand.
	if comments := tracker.Comments("mw-gq6.1"); len(comments) == 0 || !strings.Contains(comments[len(comments)-1], "given back by hand") {
		t.Errorf("expected a comment saying the claim is given back by hand, got %q", comments)
	}
}
