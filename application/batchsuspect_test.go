package application

import (
	"context"
	"testing"
	"time"
)

// branchFiles is BranchChanges that knows what each story's branch changed.
type branchFiles map[string][]string

func (b branchFiles) ChangedFiles(_ context.Context, _, _, branch string) ([]string, error) {
	return b[branch], nil
}
func (branchFiles) AddedFiles(context.Context, string, string, string) ([]AddedFile, error) {
	return nil, nil
}
func (branchFiles) DiffLines(context.Context, string, string, string, string) ([]DiffLine, error) {
	return nil, nil
}
func (branchFiles) ReadBranchFile(context.Context, string, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

// goRed is a go test run in which a test of one file fails and a test of another passes.
const goRed = `=== RUN   TestOk
--- PASS: TestOk (0.00s)
=== RUN   TestBroken
--- FAIL: TestBroken (0.00s)
    broken_test.go:12: expected 3, got 4
FAIL
FAIL	example.com/app/pkg	0.01s
`

// mw-6ww.119.11.1.2: the story whose branch alone changed the files of the
// failing tests is the batch's suspect.
func TestTheStoryWhoseBranchAloneChangedTheFailingTestsFileIsTheSuspect(t *testing.T) {
	report := func(files branchFiles, output string) (BatchLand, *BatchReport) {
		b := BatchLand{Next: Next{Changes: files}}
		r := &BatchReport{
			RedAt: time.Now(), Gate: Checked{Output: output},
			Merged: []BatchStory{{ID: "lp-1", Branch: "origin/mw/lp-1"}, {ID: "lp-2", Branch: "origin/mw/lp-2"}, {ID: "lp-3", Branch: "origin/mw/lp-3"}},
		}
		r.Queue = r.Merged
		return b, r
	}
	owned := branchFiles{
		"origin/mw/lp-1": {"pkg/a.go", "pkg/a_test.go"},
		"origin/mw/lp-2": {"pkg/broken.go", "pkg/broken_test.go"},
		"origin/mw/lp-3": {"pkg/c.go"},
	}
	b, r := report(owned, goRed)
	b.findFailingOwner(context.Background(), "/rig", r)
	if r.failingOwner != "lp-2" {
		t.Errorf("expected lp-2, the only branch to change broken_test.go, got %q", r.failingOwner)
	}

	// A test file two branches changed is no one's alone.
	shared := branchFiles{
		"origin/mw/lp-1": {"pkg/broken_test.go"},
		"origin/mw/lp-2": {"pkg/broken_test.go"},
	}
	b, r = report(shared, goRed)
	b.findFailingOwner(context.Background(), "/rig", r)
	if r.failingOwner != "" {
		t.Errorf("expected no suspect for a file two branches changed, got %q", r.failingOwner)
	}

	// A file only a passing test names is not a failing test's.
	b, r = report(owned, "--- PASS: TestOk (0.00s)\n    a_test.go:1: ok\n--- FAIL: TestOther (0.00s)\n    other_test.go:9: no\nFAIL\n")
	b.findFailingOwner(context.Background(), "/rig", r)
	if r.failingOwner != "" {
		t.Errorf("expected no suspect when no failing line names a changed test file, got %q", r.failingOwner)
	}

	// Two failing files of two branches: no one story.
	b, r = report(owned, goRed+"--- FAIL: TestA (0.00s)\n    a_test.go:3: no\n")
	b.findFailingOwner(context.Background(), "/rig", r)
	if r.failingOwner != "" {
		t.Errorf("expected no suspect when two stories' test files failed, got %q", r.failingOwner)
	}

	// A Playwright failure names its spec on the failing line itself.
	pw := branchFiles{"origin/mw/lp-1": {"e2e/login.spec.ts"}, "origin/mw/lp-3": {"e2e/home.spec.ts"}}
	b, r = report(pw, "  ✘  1 e2e/login.spec.ts:5:1 › logs in (1.2s)\n  ✓  2 e2e/home.spec.ts:7:1 › shows (0.3s)\n")
	b.findFailingOwner(context.Background(), "/rig", r)
	if r.failingOwner != "lp-1" {
		t.Errorf("expected lp-1, the only branch to change the failing spec, got %q", r.failingOwner)
	}

	// The batch's line carries it, as the fixer's word would.
	if got := r.Metric(time.Now()).Suspect; got != "lp-1" {
		t.Errorf("expected the batch line to carry the suspect, got %q", got)
	}
}
