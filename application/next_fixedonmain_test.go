package application_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// removingLanding is the fake landing that also says which worktrees and branches
// it was asked to take away.
type removingLanding struct {
	*fakeRetryLanding
	removed []string
}

func (r *removingLanding) Remove(_ context.Context, _, dir, branch string) error {
	r.removed = append(r.removed, dir+" "+branch)
	return nil
}

const alreadyFixedComment = "Already fixed on main; I made no change and committed nothing, so the Mayor should close this as fixed.\n\n" +
	"Verified (run): the spec passed 5 runs in a row on main.\n\n" +
	"Internal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing\n"

// aBuilderThatFoundTheBugFixed is a close-out of mw-x.1, a nightly-suite bug whose
// Builder committed nothing and wrote a closing comment saying the bug is fixed on
// main already (mw-gq6.869).
func aBuilderThatFoundTheBugFixed(t *testing.T, comment string) (application.Next, *apptest.FakeTracker, *removingLanding, *stubRechecks) {
	t.Helper()
	next, tracker, _ := aNextWithNothingToLand(t)
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.2", Title: "[bug] lampas main: " + greekSpec + " fails on main", Overrides: domain.Path{Files: greekSpec}})
	mustDo(t, tracker.SetLabels("mw-x.2", application.LabelBug, application.LabelSuite))
	mustDo(t, tracker.ClaimStory(context.Background(), "mw-x.2"))
	mustDo(t, tracker.CommentOnStoryAt("mw-x.2", comment, time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)))
	if _, err := next.Vault.PutRunFile(context.Background(), "mw-x.2", "result.json", plainSuccessResult); err != nil {
		t.Fatal(err)
	}
	lands := &removingLanding{fakeRetryLanding: &fakeRetryLanding{}}
	next.Worktrees, next.Landing = lands, lands
	recheck := &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed (4.1s)\n"}}
	next.Rechecks = recheck
	return next, tracker, lands, recheck
}

// mw-gq6.869: a Builder that ends with no commits and says the bug is fixed on main
// already has its spec run on main by mw next; a pass closes the story as fixed,
// takes away its worktree and branch, and needs no person.
func TestNextClosesANoCommitsBugAsFixedWhenItsSpecPassesOnMain(t *testing.T) {
	next, tracker, lands, recheck := aBuilderThatFoundTheBugFixed(t, alreadyFixedComment)

	report, err := next.Run(context.Background(), "mw-x.2")
	if err != nil {
		t.Fatalf("expected the story closed as fixed on main, got %v (%s)", err, report.String())
	}
	if len(recheck.asked) != 1 || recheck.asked[0] != "millwright "+greekSpec {
		t.Errorf("expected the spec run once on the rig's main, asked %v", recheck.asked)
	}
	if want := "already fixed on main at 9f3c2ab41: " + greekSpec + " passes (3 tests)"; tracker.CloseReason("mw-x.2") != want {
		t.Errorf("close reason:\n got %q\nwant %q", tracker.CloseReason("mw-x.2"), want)
	}
	if !report.Closed || report.Reason != "" {
		t.Errorf("expected a closed story and no refusal, got closed=%v reason=%q", report.Closed, report.Reason)
	}
	if len(lands.removed) != 1 || !strings.HasSuffix(lands.removed[0], " mw/mw-x.2") {
		t.Errorf("expected the story's worktree and branch taken away, removed %q", lands.removed)
	}
	if said := strings.Join(tracker.Comments("mw-x.2"), "\n"); !strings.Contains(said, "3 passed (4.1s)") {
		t.Errorf("expected the run's tail on the story, comments: %s", said)
	}
	if !strings.Contains(report.String(), "fixed") {
		t.Errorf("expected the report to say it was closed as fixed, got:\n%s", report.String())
	}
}

// If the spec fails on main, or the Builder did not say the bug is fixed, the
// story stays refused as no-commits with its claim and worktree, as it always did.
func TestNextLeavesANoCommitsBugAloneWhenTheSpecFailsOrTheBuilderDidNotSayFixed(t *testing.T) {
	for name, tc := range map[string]struct {
		comment string
		answer  application.SpecRecheck
		err     error
		asks    int
	}{
		"the spec still fails":      {comment: alreadyFixedComment, answer: application.SpecRecheck{Sha: "9f3c2ab41d0e", Passed: false}, asks: 1},
		"the run could not be made": {comment: alreadyFixedComment, err: errors.New("no checkout"), asks: 1},
		"the Builder did not say fixed": {
			comment: "Could not reproduce it yet.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing\n",
			answer:  application.SpecRecheck{Passed: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			next, tracker, lands, recheck := aBuilderThatFoundTheBugFixed(t, tc.comment)
			recheck.answer, recheck.err = tc.answer, tc.err

			report, err := next.Run(context.Background(), "mw-x.2")
			if err == nil || report.Reason != application.ReasonNoCommits {
				t.Fatalf("expected the refusal for no-commits, got %v and reason %q", err, report.Reason)
			}
			if len(recheck.asked) != tc.asks {
				t.Errorf("expected %d re-runs, got %v", tc.asks, recheck.asked)
			}
			if tracker.CloseReason("mw-x.2") != "" || report.Closed {
				t.Errorf("expected the story left open, closed with %q", tracker.CloseReason("mw-x.2"))
			}
			if len(lands.removed) != 0 {
				t.Errorf("expected the worktree and branch kept, removed %q", lands.removed)
			}
		})
	}
}

// mw-gq6.973: the close-out of a bug fixed on main takes the steps of the landed
// tail that a story landed with nothing to land takes, by their names in the
// table (landedtail.go), not a copy of them: the worktree, the ledger line, the
// vault commit, the close, the formula and the baton. It reads nothing of the
// branch, mails nothing, and records no benchmark, for no test of its own was run.
func TestNextClosesABugFixedOnMainWithTheLandedTailsStepsAndNoOthers(t *testing.T) {
	next, _, _, _ := aBuilderThatFoundTheBugFixed(t, alreadyFixedComment)
	trace := &tailTrace{}
	next = next.WithTailTrace(trace.add)

	if _, err := next.Run(context.Background(), "mw-x.2"); err != nil {
		t.Fatalf("expected the story closed as fixed on main, got %v", err)
	}
	want := []string{
		"take the worktree away mw-x.2", "write the ledger line mw-x.2", "commit the vault mw-x.2",
		"close the story mw-x.2", "close the formula mw-x.2", "carry on mw-x.2",
	}
	if got := trace.taken(); !reflect.DeepEqual(got, want) {
		t.Errorf("a bug closed as fixed on main took\n  %s\nexpected\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
