package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// offHostRetry is a retry run on the laptop of a story the desktop claimed
// (mw-gq6.774): the worktree is on the desktop, and remote says what it holds.
func offHostRetry(t *testing.T, remote application.WorktreeRemote) (application.Retry, *apptest.FakeTracker, *fakeRetryLanding, *strings.Builder, *strings.Builder) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	aClaimedRetryStory(t, tracker, "mw-gq6.1", "mw@desktop", "desktop")
	landing := &fakeRetryLanding{AheadCount: 2}
	var out, errs strings.Builder
	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Host = "laptop"
	retry.Reach = remote
	retry.Err = &errs
	return retry, tracker, landing, &out, &errs
}

func stillClaimedByDesktop(t *testing.T, tracker *apptest.FakeTracker) {
	t.Helper()
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "mw@desktop" {
		t.Errorf("expected the claim left with mw@desktop, got %q (status %q)", detail.Assignee, detail.Status)
	}
	if len(tracker.Comments("mw-gq6.1")) != 0 {
		t.Errorf("expected a refusal to comment nothing, got %v", tracker.Comments("mw-gq6.1"))
	}
}

func TestRetryOffTheStoryHostGivesTheClaimBackWhenItsWorktreeThereIsClean(t *testing.T) {
	remote := apptest.NewFakeWorktreeRemote()
	remote.Holds("desktop", application.WorktreeState{Path: "/home/j/.mw-worktrees/mw-gq6.1"})
	retry, tracker, landing, out, errs := offHostRetry(t, remote)

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out)
	}
	if got := remote.Asked(); len(got) != 1 || got[0] != "desktop" {
		t.Errorf("expected desktop asked once, got %v", got)
	}
	if !report.ClaimReleased || report.Elsewhere != "desktop" || report.Unsaved != "" {
		t.Errorf("expected the claim given back from desktop with nothing unsaved, got %+v", report)
	}
	if landing.bundled || landing.removed {
		t.Errorf("expected no worktree read or removed on laptop")
	}
	if errs.Len() != 0 || strings.Contains(strings.Join(tracker.Comments("mw-gq6.1"), "\n"), "unsaved") {
		t.Errorf("expected no unsaved-work warning for a clean worktree, got stderr %q comments %v", errs, tracker.Comments("mw-gq6.1"))
	}
}

func TestRetryOffTheStoryHostRefusesWhenItsWorktreeThereIsDirty(t *testing.T) {
	remote := apptest.NewFakeWorktreeRemote()
	remote.Holds("desktop", application.WorktreeState{Path: "/home/j/.mw-worktrees/mw-gq6.1", Left: []string{"application/retry.go"}})
	retry, tracker, _, out, _ := offHostRetry(t, remote)

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err == nil || !report.Refused {
		t.Fatalf("expected the retry refused, got err %v report %+v", err, report)
	}
	for _, want := range []string{"desktop", "/home/j/.mw-worktrees/mw-gq6.1", "application/retry.go", "mw retry mw-gq6.1", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got: %v", want, err)
		}
	}
	if !strings.Contains(out.String(), "REFUSED") {
		t.Errorf("expected the report to say REFUSED, got %q", out)
	}
	stillClaimedByDesktop(t, tracker)
}

func TestRetryOffTheStoryHostRefusesWhenTheHostCannotBeAsked(t *testing.T) {
	remote := apptest.NewFakeWorktreeRemote()
	remote.Unreachable("desktop", errors.New("ssh: connect to host desktop port 22: Connection refused"))
	retry, tracker, _, _, _ := offHostRetry(t, remote)

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err == nil || !report.Refused {
		t.Fatalf("expected the retry refused, got err %v report %+v", err, report)
	}
	for _, want := range []string{"desktop", "Connection refused", "mw retry mw-gq6.1", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got: %v", want, err)
		}
	}
	stillClaimedByDesktop(t, tracker)
}

func TestRetryOffTheStoryHostWithNoWayToAskRefuses(t *testing.T) {
	retry, tracker, _, _, _ := offHostRetry(t, nil)
	if _, err := retry.Run(context.Background(), "mw-gq6.1"); err == nil || !strings.Contains(err.Error(), "mw retry mw-gq6.1") {
		t.Fatalf("expected a refusal naming the command to run on desktop, got: %v", err)
	}
	stillClaimedByDesktop(t, tracker)
}

func TestForcedRetryOffTheStoryHostSaysWhatWorkIsLeftUnsavedOnStderrAndTheBead(t *testing.T) {
	remote := apptest.NewFakeWorktreeRemote()
	remote.Holds("desktop", application.WorktreeState{Path: "/home/j/.mw-worktrees/mw-gq6.1", Left: []string{"application/retry.go"}})
	retry, tracker, _, _, errs := offHostRetry(t, remote)
	retry.Force = true

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the forced retry to succeed, got: %v", err)
	}
	const want = "work left unsaved on desktop: /home/j/.mw-worktrees/mw-gq6.1"
	if report.Unsaved != want || !report.ClaimReleased {
		t.Errorf("expected the claim given back with %q, got %+v", want, report)
	}
	if !strings.Contains(errs.String(), want) {
		t.Errorf("expected stderr to say %q, got %q", want, errs)
	}
	if said := strings.Join(tracker.Comments("mw-gq6.1"), "\n"); !strings.Contains(said, want) {
		t.Errorf("expected the bead's comment to say %q, got %q", want, said)
	}
}

func TestForcedRetryOffTheStoryHostWithTheHostUnreachableStillSaysItMayHoldUnsavedWork(t *testing.T) {
	retry, tracker, _, _, errs := offHostRetry(t, apptest.NewFakeWorktreeRemote())
	retry.Force = true

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the forced retry to succeed, got: %v", err)
	}
	const want = "work left unsaved on desktop: "
	if !strings.HasPrefix(report.Unsaved, want) || !strings.Contains(errs.String(), want) {
		t.Errorf("expected %q on stderr and in the report, got stderr %q report %q", want, errs, report.Unsaved)
	}
	if said := strings.Join(tracker.Comments("mw-gq6.1"), "\n"); !strings.Contains(said, want) {
		t.Errorf("expected the bead's comment to say %q, got %q", want, said)
	}
}

func TestForcedRetryOffTheStoryHostWithACleanWorktreeSaysNothingIsUnsaved(t *testing.T) {
	remote := apptest.NewFakeWorktreeRemote()
	remote.Holds("desktop", application.WorktreeState{})
	retry, _, _, _, errs := offHostRetry(t, remote)
	retry.Force = true

	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil || report.Unsaved != "" || errs.Len() != 0 {
		t.Errorf("expected success with nothing unsaved, got err %v unsaved %q stderr %q", err, report.Unsaved, errs)
	}
}

func TestOffHostWhyNamesTheHostAndTheCommandToRunThere(t *testing.T) {
	why := application.OffHostWhy("next", "mw-gq6.1", "mw@desktop", "laptop")
	for _, want := range []string{"desktop", "laptop", "mw next mw-gq6.1"} {
		if !strings.Contains(why, want) {
			t.Errorf("expected %q in %q", want, why)
		}
	}
	for _, assignee := range []string{"", "mw@laptop", "jonathan"} {
		if why := application.OffHostWhy("next", "mw-gq6.1", assignee, "laptop"); why != "" {
			t.Errorf("claim %q: expected no refusal on laptop, got %q", assignee, why)
		}
	}
}

func TestWorktreeStateRoundTripsAndNeverReadsSilenceAsClean(t *testing.T) {
	for _, state := range []application.WorktreeState{
		{},
		{Path: "/w/mw-1"},
		{Path: "/w/mw-1", Left: []string{"a.go", "dir/b.go"}},
	} {
		got, err := application.ParseWorktreeState(state.String())
		if err != nil || got.Path != state.Path || strings.Join(got.Left, ",") != strings.Join(state.Left, ",") {
			t.Errorf("%+v: round trip gave %+v, %v", state, got, err)
		}
	}
	if _, err := application.ParseWorktreeState("Welcome to desktop\n"); err == nil {
		t.Errorf("expected text with no worktree line to be an error, not a clean worktree")
	}
}

func TestWorktreeHerePrintsThePathAndWhatIsUncommitted(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{UncommittedPaths: []string{"a.go"}}
	var out strings.Builder
	state, err := application.WorktreeHere{
		Tracker: tracker, Worktrees: landing, Landing: landing,
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, Out: &out,
	}.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Path != application.WorktreeDir("/rigs/millwright", "mw-gq6.1") || len(state.Left) != 1 {
		t.Errorf("unexpected state %+v", state)
	}
	if parsed, err := application.ParseWorktreeState(out.String()); err != nil || parsed.Path != state.Path {
		t.Errorf("expected the printed state to parse back, got %q: %v", out.String(), err)
	}

	landing.NoWorktree = true
	out.Reset()
	if state, err := (application.WorktreeHere{
		Tracker: tracker, Worktrees: landing, Landing: landing,
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, Out: &out,
	}).Run(context.Background(), "mw-gq6.1"); err != nil || state.Path != "" || !state.Clean() {
		t.Errorf("expected no worktree to read as clean, got %+v: %v", state, err)
	}
}

// mw-gq6.774: a check of a story another host claimed has no worktree to run the
// rig's tests in here, and says which host to run it on. (mw next is not
// refused the same way: it lands and closes stories another host claimed, as a
// batch lands them, mw-gq6.475.)
func TestCheckOfAStoryAnotherHostClaimedRefusesNamingTheHost(t *testing.T) {
	_, tracker, _ := aNextWithNothingToLand(t)
	if err := tracker.ClaimAs("mw-x.1", "mw@desktop", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	lands := &fakeRetryLanding{}
	_, err := application.Check{
		Tracker: tracker, Landing: lands, Checks: checkStub{},
		Host: "laptop", Rigs: map[string]string{"millwright": "/rigs/millwright"},
	}.Run(context.Background(), "mw-x.1")
	if err == nil || !strings.Contains(err.Error(), "mw check mw-x.1` on desktop") {
		t.Fatalf("expected mw check to refuse naming desktop and its own command, got %v", err)
	}
}
