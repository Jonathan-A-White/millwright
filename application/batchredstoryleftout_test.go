package application_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-qqxlwm3.6: one story's red tests on the merged batch held its green
// stories for a whole gate, and again through a failed fixer, before the drain
// landed them in parts. The batch that can name the story whose tests failed
// leaves it out, lands the green rest first (tested as merged without it), and
// keeps the red one queued with a note naming its failing tests.

// aRebuildingLanding is a landing that says where it opened and closed worktrees,
// and which branches each worktree merged.
type aRebuildingLanding struct {
	aBranchConflictLanding
	mu4    sync.Mutex
	opened []string
	closed []string
	pushIn []string
	dirOf  map[string][]string // a worktree -> the branches merged in it
}

func (l *aRebuildingLanding) OpenLanding(context.Context, string, string) (string, error) {
	l.mu4.Lock()
	defer l.mu4.Unlock()
	dir := "/rigs/millwright/.landing-again-" + string(rune('a'+len(l.opened)))
	l.opened = append(l.opened, dir)
	return dir, nil
}

func (l *aRebuildingLanding) CloseLanding(_ context.Context, _, dir string) error {
	l.mu4.Lock()
	defer l.mu4.Unlock()
	l.closed = append(l.closed, dir)
	return nil
}

func (l *aRebuildingLanding) Merge(ctx context.Context, dir, branch string) (application.Landed, error) {
	l.mu4.Lock()
	if l.dirOf == nil {
		l.dirOf = map[string][]string{}
	}
	l.dirOf[dir] = append(l.dirOf[dir], branch)
	l.mu4.Unlock()
	return l.aBranchConflictLanding.Merge(ctx, dir, branch)
}

func (l *aRebuildingLanding) Push(ctx context.Context, dir, remote, branch string) error {
	l.mu4.Lock()
	l.pushIn = append(l.pushIn, dir)
	l.mu4.Unlock()
	return l.aBranchConflictLanding.Push(ctx, dir, remote, branch)
}

// redBranchFiles is BranchChanges that knows what each story's branch changed.
type redBranchFiles map[string][]string

func (b redBranchFiles) ChangedFiles(_ context.Context, _, _, branch string) ([]string, error) {
	return b[branch], nil
}
func (redBranchFiles) AddedFiles(context.Context, string, string, string) ([]application.AddedFile, error) {
	return nil, nil
}
func (redBranchFiles) DiffLines(context.Context, string, string, string, string) ([]application.DiffLine, error) {
	return nil, nil
}
func (redBranchFiles) ReadBranchFile(context.Context, string, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

// aRedStoryBatch is a batch of three whose first gate is red on the failing
// tests of mw-e.2's test file, and whose gate is green without it.
type aRedStoryBatch struct {
	aSetAsideBatch
	rebuilding *aRebuildingLanding
}

const redStoryGateOutput = `=== RUN   TestOk
--- PASS: TestOk (0.00s)
=== RUN   TestBroken
--- FAIL: TestBroken (0.00s)
    broken_test.go:12: expected 3, got 4
FAIL
FAIL	example.com/app/pkg	0.01s
`

// aBatchOfThreeWithARedStory is a batch of mw-e.1 to mw-e.3 in which only mw-e.2's
// branch changed broken_test.go, the file the failing test is in. ownerNamed says
// the branches' changed files are known, so that the gate names the story;
// the fixer, when result is set, ends as it says.
func aBatchOfThreeWithARedStory(t *testing.T, ownerNamed bool, result string) aRedStoryBatch {
	t.Helper()
	a := aBatchOf(t, 3)
	rebuilding := &aRebuildingLanding{aBranchConflictLanding: aBranchConflictLanding{conflicts: map[string]bool{}}}
	rebuilding.AheadCount = 1
	a.land.Next.Landing = rebuilding
	a.checks.passes = []bool{false, true}
	a.checks.outputs = []string{redStoryGateOutput, "ok\n"}
	if ownerNamed {
		a.land.Next.Changes = redBranchFiles{
			"origin/mw/mw-e.1": {"pkg/a.go", "pkg/a_test.go"},
			"origin/mw/mw-e.2": {"pkg/broken.go", "pkg/broken_test.go"},
			"origin/mw/mw-e.3": {"pkg/c.go"},
		}
	}
	if result != "" {
		vault := newFakeVault()
		vault.written["batch-millwright-595/"+application.ResultFileName] = result
		a.land.Next.Vault = vault
		a.land.Next.Boot = application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"}
		a.runner = endedRunner{apptest.NewFakeRunner(), application.SessionStatus{State: application.StateExited}}
		a.land.Next.Runner = a.runner
		a.land.Next.Out, a.land.Next.Err = io.Discard, io.Discard
	}
	return aRedStoryBatch{aSetAsideBatch: a, rebuilding: rebuilding}
}

func TestARedStoryLeftOutOfABatchOfThreeLetsTheTwoGreenOnesLandFirst(t *testing.T) {
	a := aBatchOfThreeWithARedStory(t, true, "")
	a.run(t)

	r := a.report
	if !r.Landed || r.Held != "" {
		t.Fatalf("expected the two green stories landed, got landed %v, held %q", r.Landed, r.Held)
	}
	if got := strings.Join(landedIDs(r), ","); got != "mw-e.1,mw-e.3" {
		t.Errorf("expected mw-e.1 and mw-e.3 landed, got %s", got)
	}
	if len(r.SetAside) != 1 || r.SetAside[0].ID != "mw-e.2" {
		t.Fatalf("expected mw-e.2 set aside, got %+v", r.SetAside)
	}
	if a.rebuilding.pushed != 1 {
		t.Errorf("expected one push, of the two green stories, got %d", a.rebuilding.pushed)
	}
	if a.checks.runs != 2 {
		t.Errorf("expected the rig's tests run on all three, then on the two as merged: 2 runs, got %d", a.checks.runs)
	}
	if got := a.rebuilding.dirOf[a.rebuilding.pushIn[0]]; strings.Join(got, ",") != "origin/mw/mw-e.1,origin/mw/mw-e.3" {
		t.Errorf("expected what was pushed to be made of mw-e.1 and mw-e.3 alone, got %v", got)
	}
	if len(a.rebuilding.closed) == 0 || a.rebuilding.closed[0] != "/rigs/millwright/.landing-595" {
		t.Errorf("expected the first landing worktree closed when the batch was made again, got %v", a.rebuilding.closed)
	}
	if got := a.runner.Started(); len(got) != 0 {
		t.Errorf("expected no fixer started, got %v", got)
	}
	comments := a.tracker.Comments("mw-e.2")
	if len(comments) != 1 || !strings.Contains(comments[0], "Set aside by batch 595 of millwright") ||
		!strings.Contains(comments[0], "TestBroken") || !strings.Contains(comments[0], "stays queued") {
		t.Errorf("expected a note on mw-e.2 naming its failing tests and that it stays queued, got %q", comments)
	}
	for _, id := range []string{"mw-e.1", "mw-e.3"} {
		if got := a.tracker.Comments(id); len(got) != 0 {
			t.Errorf("expected no note on %s, which landed, got %q", id, got)
		}
	}
	// A story left out for its red is not counted as one set aside for a
	// conflict, which would hand it to a Builder to rebase.
	for _, s := range r.SetAside {
		if s.ConflictSetAsides != 0 || s.HandedBack {
			t.Errorf("expected %s not counted as a conflict set-aside, got %+v", s.ID, s)
		}
	}
	if m := r.Metric(time.Now()); m.Outcome != application.BatchGreen || m.SetAside != 1 || len(m.IDs) != 2 {
		t.Errorf("expected a green batch line of two stories with one set aside, got %+v", m)
	}
}

// A gate that cannot name the story whose tests failed holds the batch as it did.
func TestAGateThatNamesNoRedStoryLeavesNoStoryOutAndHoldsTheBatchAsBefore(t *testing.T) {
	a := aBatchOfThreeWithARedStory(t, false, "")
	a.land.Fix = false
	a.run(t)

	if a.report.Landed || !strings.Contains(a.report.Held, "failed") {
		t.Fatalf("expected the batch held red, got landed %v, held %q", a.report.Landed, a.report.Held)
	}
	if a.rebuilding.pushed != 0 || len(a.report.SetAside) != 0 || a.checks.runs != 1 {
		t.Errorf("expected nothing pushed, nothing set aside and one run, got %d pushes, %v set aside, %d runs", a.rebuilding.pushed, a.report.SetAside, a.checks.runs)
	}
}

// A batch that is red again without the story left out is held, with no second
// leaving out and no push.
func TestABatchStillRedWithoutTheRedStoryLeftOutIsHeldWithNothingPushed(t *testing.T) {
	a := aBatchOfThreeWithARedStory(t, true, "")
	a.checks.passes = []bool{false}
	a.checks.outputs = []string{redStoryGateOutput}
	a.land.Fix = false
	a.run(t)

	if a.report.Landed || a.rebuilding.pushed != 0 {
		t.Fatalf("expected the batch held with nothing pushed, got landed %v, %d pushes", a.report.Landed, a.rebuilding.pushed)
	}
	if a.checks.runs != 2 {
		t.Errorf("expected the batch tested twice at most (all three, then the two), got %d runs", a.checks.runs)
	}
	if m := a.report.Metric(time.Now()); m.Outcome != application.BatchHeld || !m.Red || m.Suspect != "" {
		t.Errorf("expected a held red line naming no suspect, the two being red without it, got %+v", m)
	}
}

// A red batch whose fixer fails, and whose fixer named the story that made it
// red, lands the green part without waiting for the drain.
func TestARedStoryLeftOutOfABatchWhoseFixerFailedLetsTheGreenPartLandWithoutTheDrain(t *testing.T) {
	a := aBatchOfThreeWithARedStory(t, false, `{"subtype":"error_max_turns","is_error":true,"result":"I ran out.\nCAUSE mw-e.2: its test expects 3"}`)
	a.run(t)

	r := a.report
	if !r.Landed || r.Held != "" {
		t.Fatalf("expected the green part landed though the fixer failed, got landed %v, held %q", r.Landed, r.Held)
	}
	if got := strings.Join(landedIDs(r), ","); got != "mw-e.1,mw-e.3" {
		t.Errorf("expected mw-e.1 and mw-e.3 landed, got %s", got)
	}
	if len(r.SetAside) != 1 || r.SetAside[0].ID != "mw-e.2" {
		t.Fatalf("expected mw-e.2 set aside, got %+v", r.SetAside)
	}
	if a.rebuilding.pushed != 1 || a.checks.runs != 2 {
		t.Errorf("expected one push after a second run of the tests, got %d pushes, %d runs", a.rebuilding.pushed, a.checks.runs)
	}
	if got := a.runner.Started(); len(got) != 1 {
		t.Errorf("expected the one fixer the batch is given and no second, got %v", got)
	}
	comments := a.tracker.Comments("mw-e.2")
	if len(comments) != 1 || !strings.Contains(comments[0], "Set aside by batch 595 of millwright") || !strings.Contains(comments[0], "stays queued") {
		t.Errorf("expected a note on mw-e.2 saying it was left out and stays queued, got %q", comments)
	}
}

// A fixer that fails and names no one leaves the batch held, to be landed in parts.
func TestAFailedFixerThatNamedNoRedStoryLeavesTheBatchHeldForTheDrain(t *testing.T) {
	a := aBatchOfThreeWithARedStory(t, false, `{"subtype":"error_max_turns","is_error":true,"result":"I ran out."}`)
	a.run(t)

	if a.report.Landed || !strings.Contains(a.report.Held, "fixer ran out of turns") {
		t.Fatalf("expected the batch held for the failed fixer, got landed %v, held %q", a.report.Landed, a.report.Held)
	}
	if a.rebuilding.pushed != 0 || a.checks.runs != 1 {
		t.Errorf("expected nothing pushed and no second run, got %d pushes, %d runs", a.rebuilding.pushed, a.checks.runs)
	}
}

func landedIDs(r *application.BatchReport) []string {
	var ids []string
	for _, s := range r.Merged {
		ids = append(ids, s.ID)
	}
	return ids
}

// aLandingThatCannotBeMadeAgain opens the batch's first worktree and no other.
type aLandingThatCannotBeMadeAgain struct{ *aRebuildingLanding }

func (l *aLandingThatCannotBeMadeAgain) OpenLanding(context.Context, string, string) (string, error) {
	return "", io.ErrClosedPipe
}

// mw-qqxlwm3.7: findFailingOwner was never called, so a held batch's line did not
// carry its own suspect. A batch held red that could not be made again without
// the story whose tests failed still names it, from the failing tests' files.
func TestAHeldRedBatchLineCarriesTheStoryWhoseTestsFailedAsItsSuspect(t *testing.T) {
	a := aBatchOfThreeWithARedStory(t, true, "")
	a.land.Next.Landing = &aLandingThatCannotBeMadeAgain{a.rebuilding}
	a.checks.passes = []bool{false}
	a.checks.outputs = []string{redStoryGateOutput}
	a.land.Fix = false
	a.run(t)

	if a.report.Landed {
		t.Fatalf("expected the batch held, got landed with %q", a.report.Held)
	}
	if m := a.report.Metric(time.Now()); m.Outcome != application.BatchHeld || !m.Red || m.Suspect != "mw-e.2" {
		t.Errorf("expected a held red line naming mw-e.2 as its suspect, got %+v", m)
	}
}
