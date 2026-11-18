package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1436: a story whose work and closing comment were done, whose claim
// then went stale and was given back, was dispatched again with a formula of
// open steps, and mw next --no-dispatch --met-by-mayor was refused for them: a
// fresh Builder session for a story already closed out by comment.

// aCommittedLanding is a landing that fast-forwards and says the story's branch
// was last committed to at at.
type aCommittedLanding struct {
	aLandingLanding
	at time.Time
}

func (l *aCommittedLanding) Commits(context.Context, string, string, string) ([]application.Commit, error) {
	return []application.Commit{{Hash: "abc1234", Committed: l.at}}, nil
}

const aMayorsClosingComment = "Built it, the Mayor read it.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing"

// aStoryGivenBackAfterItsLeaseRanOut runs a dispatch over mw-gq6.9, whose session
// died and whose lease ran out, with a closing comment written on it after the
// branch's newest commit when withComment says so. The dispatch gives the claim
// back and starts the story again with a fresh formula of four open steps.
func aStoryGivenBackAfterItsLeaseRanOut(t *testing.T, withComment bool) (application.Dispatch, *apptest.FakeTracker, *aCommittedLanding, application.DispatchReport) {
	t.Helper()
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddFormula("tdd-feature",
		application.FormulaStep{Title: "Understand {{story}}: {{title}}"},
		application.FormulaStep{Title: "Implement until green"},
		application.FormulaStep{Title: "Commit"},
		application.FormulaStep{Title: "Write the closing comment"},
	)
	now := time.Date(2026, 11, 17, 14, 41, 0, 0, time.UTC)
	committed := now.Add(-30 * time.Hour)
	lands := &aCommittedLanding{aLandingLanding{fakeRetryLanding{AheadCount: 1}}, committed}
	dispatch.Now, dispatch.Landing = func() time.Time { return now }, lands
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.9", now.Add(-33*time.Hour)))
	if withComment {
		mustDo(t, tracker.CommentOnStoryAt("mw-gq6.9", aMayorsClosingComment, committed.Add(time.Hour)))
	}
	runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 1)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the stale claim to be given back, got %v", err)
	}
	if len(report.Reclaimed) != 1 {
		t.Fatalf("expected mw-gq6.9's claim given back, got %+v", report)
	}
	return dispatch, tracker, lands, report
}

// theMayorLands is mw next --no-dispatch on mw-gq6.9, with the Mayor's flags.
func theMayorLands(t *testing.T, tracker *apptest.FakeTracker, lands application.Landing, flags func(*application.Next)) (application.NextReport, error) {
	t.Helper()
	vault := newFakeVault()
	vault.written["mw-gq6.9/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	next := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands.(application.Worktrees), Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		Seat: "builder", Host: "vps",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}
	flags(&next)
	return next.Run(context.Background(), "mw-gq6.9")
}

func openStepsOf(t *testing.T, tracker *apptest.FakeTracker) int {
	t.Helper()
	molecule, poured := tracker.Poured("mw-gq6.9")
	if !poured {
		t.Fatalf("expected a formula poured for mw-gq6.9")
	}
	open, err := tracker.OpenSteps(context.Background(), molecule.RootID)
	mustDo(t, err)
	return len(open)
}

func TestGivenBackStoryWithAClosingCommentLandsWithNoDispatchMetByMayor(t *testing.T) {
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)

	// The give-back says it found the closing comment and names the way to land.
	said := strings.Join(tracker.Comments("mw-gq6.9"), "\n")
	for _, want := range []string{"closing comment", "mw next mw-gq6.9 --no-dispatch", "--met-by-mayor", "--rescoped-by-mayor"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the give-back's comment to say %q, got:\n%s", want, said)
		}
	}
	if openStepsOf(t, tracker) == 0 {
		t.Fatalf("expected the fresh formula's steps open, as the dispatch left them")
	}

	report, err := theMayorLands(t, tracker, lands, func(n *application.Next) {
		n.MetByMayor, n.MetByMayorGiven = "read the closing comment and ran the tests", true
	})
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed with no session, got err %v and %+v", err, report)
	}
	if open := openStepsOf(t, tracker); open != 0 {
		t.Errorf("expected no formula step left open, got %d", open)
	}
}

func TestGivenBackStoryWithAClosingCommentAlsoLandsWithNoDispatchRescopedByMayor(t *testing.T) {
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)
	report, err := theMayorLands(t, tracker, lands, func(n *application.Next) { n.RescopedByMayor = "reworded" })
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed with no session, got err %v and %+v", err, report)
	}
	if open := openStepsOf(t, tracker); open != 0 {
		t.Errorf("expected no formula step left open, got %d", open)
	}
}

func TestGivenBackStoryWithNoClosingCommentIsRefusedForOpenStepsNamingTheMayorsFlags(t *testing.T) {
	// Without the Mayor's word, and with it but no closing comment, the steps stay open.
	for name, flags := range map[string]func(*application.Next){
		"no flag":  func(*application.Next) {},
		"met":      func(n *application.Next) { n.MetByMayor, n.MetByMayorGiven = "ran it", true },
		"rescoped": func(n *application.Next) { n.RescopedByMayor = "reworded" },
	} {
		_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, false)
		if said := strings.Join(tracker.Comments("mw-gq6.9"), "\n"); strings.Contains(said, "--met-by-mayor") {
			t.Errorf("%s: expected the give-back to name no way to land without a closing comment, got:\n%s", name, said)
		}
		report, _ := theMayorLands(t, tracker, lands, flags)
		if report.Landed || report.Reason != application.ReasonOpenSteps {
			t.Fatalf("%s: expected a refusal for %s, got %+v", name, application.ReasonOpenSteps, report)
		}
		for _, flag := range []string{"--rescoped-by-mayor", "--met-by-mayor"} {
			if !strings.Contains(report.Why, flag) {
				t.Errorf("%s: expected the refusal to name %s, got:\n%s", name, flag, report.Why)
			}
		}
	}
}

func TestGivenBackStoryWithAClosingCommentIsStillRefusedForOpenStepsWithoutTheMayorsWord(t *testing.T) {
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)
	report, _ := theMayorLands(t, tracker, lands, func(*application.Next) {})
	if report.Landed || report.Reason != application.ReasonOpenSteps {
		t.Fatalf("expected a refusal for %s, got %+v", application.ReasonOpenSteps, report)
	}
	if !strings.Contains(report.Why, "--met-by-mayor") {
		t.Errorf("expected the refusal to name the way out, got:\n%s", report.Why)
	}
}
