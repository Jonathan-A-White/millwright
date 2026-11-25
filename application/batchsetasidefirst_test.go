package application_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-qqxlwm3.2: lampas batch 595 merged five stories cleanly and set aside one
// for a conflict; the five waited about 13 hours for its fixer. A batch of which
// some branches merge lands them first, tested as merged, and the branches set
// aside stay queued with a note: only a batch of which every branch conflicts is
// given its fixer before the push.

// aBranchConflictLanding merges every branch but the ones it is told conflict, and
// says what was pushed.
type aBranchConflictLanding struct {
	fakeRetryLanding
	conflicts map[string]bool
	// besideMates are the conflicting branches that merge into the target alone.
	besideMates map[string]bool
	mu3         sync.Mutex
	merged      []string
	pushed      int
}

// trialDir is where the fake cuts a landing of its own, apart from the batch's:
// where a branch is tried on the target branch alone.
const trialDir = "/rigs/millwright/.landing-trial"

func (l *aBranchConflictLanding) OpenLanding(context.Context, string, string) (string, error) {
	return trialDir, nil
}

// onlyBesideMates makes story id's conflict one with the batch's other stories:
// its branch merges into the target branch alone.
func (l *aBranchConflictLanding) onlyBesideMates(id string) {
	l.mu3.Lock()
	defer l.mu3.Unlock()
	l.besideMates["origin/mw/"+id] = true
}

func (l *aBranchConflictLanding) Merge(_ context.Context, dir, branch string) (application.Landed, error) {
	l.mu3.Lock()
	defer l.mu3.Unlock()
	if l.conflicts[branch] && !(dir == trialDir && l.besideMates[branch]) {
		return application.Landed{}, fmt.Errorf("%w: CONFLICT (content): Merge conflict in a_test.go", application.ErrMergeConflict)
	}
	l.merged = append(l.merged, branch)
	return application.Landed{Commit: "merge-" + branch}, nil
}

func (l *aBranchConflictLanding) Push(context.Context, string, string, string) error {
	l.mu3.Lock()
	defer l.mu3.Unlock()
	l.pushed++
	return nil
}

func (l *aBranchConflictLanding) RefHead(ctx context.Context, dir, ref string) (string, error) {
	if ref == "HEAD" {
		return "batchcommit1", nil
	}
	return l.fakeRetryLanding.RefHead(ctx, dir, ref)
}

type aSetAsideBatch struct {
	land    application.BatchLand
	landing *aBranchConflictLanding
	checks  *scriptedChecks
	runner  endedRunner
	tracker *apptest.FakeTracker
	report  *application.BatchReport
}

// aBatchOf makes a batch of n stories, mw-1 to mw-n, of which the ones named in
// conflicting conflict with the batch, with a fixer that may be started.
func aBatchOf(t *testing.T, n int, conflicting ...string) aSetAsideBatch {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	landing := &aBranchConflictLanding{conflicts: map[string]bool{}, besideMates: map[string]bool{}}
	landing.AheadCount = 1 // each branch holds work beyond the base, and none is in a fixer's batch
	report := &application.BatchReport{Rig: "millwright", Target: "main", Number: 595, Base: "base0123"}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("mw-e.%d", i)
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: "The story " + id})
		report.Queue = append(report.Queue, application.BatchStory{ID: id, Title: "The story " + id, Branch: "origin/mw/" + id})
	}
	for _, id := range conflicting {
		landing.conflicts["origin/mw/"+id] = true
	}
	vault := newFakeVault()
	checks := &scriptedChecks{passes: []bool{true}}
	runner := endedRunner{apptest.NewFakeRunner(), application.SessionStatus{State: application.StateExited}}
	land := application.BatchLand{
		Next: application.Next{
			Tracker: tracker, Vault: vault, Landing: landing, Runner: runner, Checks: checks,
			Boot: application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
			Host: "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
		},
		Records: noRecords{}, Fix: true,
	}
	return aSetAsideBatch{land: land, landing: landing, checks: checks, runner: runner, tracker: tracker, report: report}
}

func (a aSetAsideBatch) run(t *testing.T) {
	t.Helper()
	a.land.LandTheBatchIn(context.Background(), "/rigs/millwright/.landing-595", a.report)
}

func TestABatchOfSixThatSetsOneAsideLandsTheFiveFirstAndLeavesTheOneQueuedWithANote(t *testing.T) {
	a := aBatchOf(t, 6, "mw-e.4")
	a.landing.onlyBesideMates("mw-e.4")
	aQueuedInTheTracker(t, a)
	a.run(t)

	r := a.report
	if !r.Landed || r.Held != "" {
		t.Fatalf("expected the five landed, got landed %v, held %q", r.Landed, r.Held)
	}
	if a.landing.pushed != 1 || len(r.Merged) != 5 || len(r.SetAside) != 1 || r.SetAside[0].ID != "mw-e.4" {
		t.Fatalf("expected one push of five with mw-e.4 set aside, got %d pushes, merged %d, set aside %+v", a.landing.pushed, len(r.Merged), r.SetAside)
	}
	if a.checks.runs != 1 {
		t.Errorf("expected the rig's tests run once on the five as merged, got %d runs", a.checks.runs)
	}
	if got := a.runner.Started(); len(got) != 0 {
		t.Errorf("expected no fixer started before the push, got %v", got)
	}
	if r.Fixer != nil {
		t.Errorf("expected the batch given no fixer, got %+v", r.Fixer)
	}
	comments := a.tracker.Comments("mw-e.4")
	if len(comments) != 1 || !strings.Contains(comments[0], "Set aside by batch 595 of millwright") ||
		!strings.Contains(comments[0], "Merge conflict in a_test.go") || !strings.Contains(comments[0], "stays queued") {
		t.Errorf("expected a note on mw-e.4 saying why it was set aside and that it stays queued, got %q", comments)
	}
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.3", "mw-e.5", "mw-e.6"} {
		if got := a.tracker.Comments(id); len(got) != 0 {
			t.Errorf("expected no note on %s, which landed, got %q", id, got)
		}
	}
	if joined := strings.Join(r.Notes, "\n"); !strings.Contains(joined, "mw-e.4 was set aside by batch 595 of millwright") || !strings.Contains(joined, "5 stories landed") {
		t.Errorf("expected a note in the report saying the five landed without it, got %q", r.Notes)
	}
}

// Only a batch of which every branch conflicts waits for its fixer before a push:
// there is nothing green to hold.
func TestABatchOfWhichEveryBranchConflictsIsStillGivenItsFixerBeforeThePush(t *testing.T) {
	a := aBatchOf(t, 2, "mw-e.1", "mw-e.2")
	a.run(t)

	if a.report.Fixer == nil || !a.report.Fixer.Started {
		t.Fatalf("expected the fixer started for a batch of which nothing merged, got %+v (held %q)", a.report.Fixer, a.report.Held)
	}
	if a.landing.pushed != 0 {
		t.Errorf("expected nothing pushed, the fixer having merged nothing, got %d pushes", a.landing.pushed)
	}
}

// A batch with nothing set aside leaves no note on any story.
func TestABatchWithNothingSetAsideLeavesNoNoteOnAnyStory(t *testing.T) {
	a := aBatchOf(t, 3)
	a.run(t)
	if !a.report.Landed {
		t.Fatalf("expected the batch landed, got held %q", a.report.Held)
	}
	for _, s := range a.report.Queue {
		if got := a.tracker.Comments(s.ID); len(got) != 0 {
			t.Errorf("expected no note on %s, got %q", s.ID, got)
		}
	}
}

// A fixer given a red batch is not blamed on a story set aside beside it: that
// story is not in the batch, and its conflict is not what the fixer was to mend.
func TestAFailedFixerOfARedBatchIsNotBlamedOnAStorySetAsideBesideIt(t *testing.T) {
	got := aFixerThatEnded(t, application.SessionStatus{State: application.StateExited}, `{"subtype":"error_max_turns","is_error":true}`)
	got.report.Fixer.Tested = true
	got.report.Held = ""
	_, mail := got.land.FixerReads(got.report)
	if strings.Contains(mail, "was set aside") || strings.Contains(mail, "the story set aside") {
		t.Errorf("expected the cause to be the red tests, not the set-aside story, got:\n%s", mail)
	}
	if !strings.Contains(mail, "the rig's tests failed on the batch") {
		t.Errorf("expected the cause to say the rig's tests failed on the batch, got:\n%s", mail)
	}
}
