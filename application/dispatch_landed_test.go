package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.161: a story whose close-out merged its branch but never reached the
// bead close (the Laptop, home, was unreachable) was found by the dead-pane
// reclaim with its lease run out and given back, to be worked a second time on
// top of its own landed commits. These tests walk the reclaim through a branch
// that is merged, one that is not, and one git cannot be asked about.

const landedTip = "911b4f5c0ffee0123456789abcdef0123456789a"

// aDeadPaneWithALapsedLease is a story claimed here whose window has died and
// whose lease ran out half an hour before now, with the story's branch as the
// given landing says it is.
func aDeadPaneWithALapsedLease(t *testing.T, landing *fakeRetryLanding) (application.Dispatch, *apptest.FakeTracker, *apptest.FakeRunner, string) {
	t.Helper()
	dispatch, tracker, _, runner, _ := aFactory(t)
	now := time.Date(2026, 9, 30, 23, 20, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Landing = landing
	claimedStory(t, tracker, "mw-gq6.9", 1)
	if err := tracker.SetLeaseExpires("mw-gq6.9", now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("setting the lease: %v", err)
	}
	session := startOldSession(t, runner, "mw-gq6.9")
	runner.Exit(session, 1)
	return dispatch, tracker, runner, session
}

func TestReclaimDeadPaneClosesAStoryWhoseBranchAlreadyLanded(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, runner, session := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{MergedTip: landedTip})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if report.Running != 0 {
		t.Fatalf("expected the landed story not counted running, got %+v", report)
	}
	if len(report.Reclaimed) != 0 || len(report.Started) != 0 {
		t.Fatalf("expected the landed story neither given back nor started again, got reclaimed %+v started %+v", report.Reclaimed, report.Started)
	}
	if len(report.LandedAlready) != 1 || report.LandedAlready[0].StoryID != "mw-gq6.9" ||
		report.LandedAlready[0].Tip != landedTip || report.LandedAlready[0].Target != "main" {
		t.Fatalf("expected mw-gq6.9 reported landed already on main at %s, got %+v", landedTip, report.LandedAlready)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusClosed {
		t.Fatalf("expected the landed story closed, got %q", detail.Status)
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunLanded {
		t.Fatalf("expected the story recorded %s=%s, got %q", application.RunState, application.RunLanded, state)
	}
	comments := strings.Join(tracker.Comments("mw-gq6.9"), "\n")
	for _, want := range []string{landedTip[:12], "main", "already"} {
		if !strings.Contains(comments, want) {
			t.Fatalf("expected a comment naming %q, got %q", want, comments)
		}
	}
	if closed := runner.Closed(); len(closed) != 1 || closed[0] != session {
		t.Fatalf("expected the dead window closed, got %q closed", closed)
	}
	printed := report.String()
	if !strings.Contains(printed, "mw-gq6.9") || !strings.Contains(printed, "already landed") {
		t.Fatalf("expected the printed report to say mw-gq6.9 had already landed, got:\n%s", printed)
	}
}

func TestReclaimDeadPaneGivesBackAStoryWhoseBranchIsNotMerged(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, session := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the story to be reclaimed and redispatched, got %v", err)
	}
	assertReclaimedAsBefore(t, report, tracker, session)
}

func TestReclaimDeadPaneGivesBackAStoryWhenGitCannotBeRead(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, session := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{MergedErr: fmt.Errorf("not a git repository")})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the story to be reclaimed and redispatched, got %v", err)
	}
	assertReclaimedAsBefore(t, report, tracker, session)
}

func TestReclaimDeadPaneLeavesALandedStoryAloneWhileItsLeaseHolds(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{MergedTip: landedTip})
	if err := tracker.SetLeaseExpires("mw-gq6.9", dispatch.Now().Add(time.Hour)); err != nil {
		t.Fatalf("setting the lease: %v", err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if report.Running != 1 || len(report.LandedAlready) != 0 || len(report.Reclaimed) != 0 {
		t.Fatalf("expected the claim left counted running while its lease holds, got %+v", report)
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-gq6.9"); detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the claim left as it was, got %q", detail.Status)
	}
}

func TestReclaimDeadPaneKeepsTheClaimOfALandedStoryItCouldNotClose(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, session := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{MergedTip: landedTip})
	// An open step under the story is one thing the tracker refuses to close it over.
	tracker.AddStory("mw-gq6.9", domain.Story{ID: "mw-gq6.9.1", Title: "A step"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 0 || len(report.Reclaimed) != 0 || len(report.LandedAlready) != 0 {
		t.Fatalf("expected a landed story neither started again nor given back, got %+v", report)
	}
	if report.Running != 1 || len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "claim was kept") {
		t.Fatalf("expected the claim counted running with one note saying it was kept, got running %d notes %q", report.Running, report.Notes)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Fatalf("expected the claim kept for a later tick to close, got %q held by %q", detail.Status, detail.Assignee)
	}
	if len(tracker.Comments("mw-gq6.9")) != 0 {
		t.Fatalf("expected nothing written to a story that was not closed, got %q", tracker.Comments("mw-gq6.9"))
	}
	if closed := dispatch.Runner.(*apptest.FakeRunner).Closed(); len(closed) != 0 {
		t.Fatalf("expected the window left while the story stays claimed, got %q closed (%s)", closed, session)
	}
}

// assertReclaimedAsBefore is the outcome the reclaim had before it asked about
// the branch: the claim given back and the story dispatched again.
func assertReclaimedAsBefore(t *testing.T, report application.DispatchReport, tracker *apptest.FakeTracker, session string) {
	t.Helper()
	if len(report.LandedAlready) != 0 {
		t.Fatalf("expected nothing reported landed already, got %+v", report.LandedAlready)
	}
	if len(report.Reclaimed) != 1 || report.Reclaimed[0].StoryID != "mw-gq6.9" || report.Reclaimed[0].Session != session {
		t.Fatalf("expected mw-gq6.9's claim given back, got %+v", report.Reclaimed)
	}
	if len(report.Started) != 1 || report.Started[0].Attempt != 1 {
		t.Fatalf("expected mw-gq6.9 started again as attempt 1, the one it ended refunded, got %+v", report.Started)
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status == apptest.StatusClosed {
		t.Fatalf("expected the story left open for its next attempt, got closed")
	}
}

// mw-gq6.191: mw-a0ih0.11 landed, its run=landed was written, and then the
// close failed (the tracker's server was unreachable). The landing had already
// taken its branch away, so the branch check above found nothing merged, and
// the dead-pane reclaim gave the claim back: the story was claimed again and
// worked a third time on top of its own landed commit. These tests walk the
// reclaim and the ready list through a story whose last run landed it.

func TestReclaimDeadPaneClosesAStoryRecordedLandedWhoseBranchIsGone(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, runner, session := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{})
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunLanded, "landed on main (fast-forward, b34796772efe)"))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Reclaimed) != 0 || len(report.Started) != 0 {
		t.Fatalf("expected a landed story neither given back nor started again, got reclaimed %+v started %+v", report.Reclaimed, report.Started)
	}
	if report.Running != 0 {
		t.Fatalf("expected the landed story not counted running, got %+v", report)
	}
	if len(report.LandedAlready) != 1 || report.LandedAlready[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected mw-gq6.9 reported landed already, got %+v", report.LandedAlready)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusClosed {
		t.Fatalf("expected the landed story closed, got %q", detail.Status)
	}
	if detail.Attempts != 1 {
		t.Fatalf("expected the attempts left at 1, got %d", detail.Attempts)
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunLanded {
		t.Fatalf("expected the story still recorded %s=%s, got %q", application.RunState, application.RunLanded, state)
	}
	comments := strings.Join(tracker.Comments("mw-gq6.9"), "\n")
	if !strings.Contains(comments, "run=landed") || strings.Contains(comments, "given back") {
		t.Fatalf("expected a comment saying the story was closed because its run was recorded landed, got %q", comments)
	}
	if closed := runner.Closed(); len(closed) != 1 || closed[0] != session {
		t.Fatalf("expected the dead window closed, got %q closed", closed)
	}
	printed := report.String()
	if !strings.Contains(printed, "mw-gq6.9") || !strings.Contains(printed, "already landed") || !strings.Contains(printed, "run=landed") {
		t.Fatalf("expected the printed report to say mw-gq6.9 had already landed by its run state, got:\n%s", printed)
	}
}

func TestReclaimDeadPaneKeepsTheClaimOfAStoryRecordedLandedItCouldNotClose(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, runner, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{})
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunLanded, "landed on main"))
	tracker.RefuseToClose("mw-gq6.9", "Dolt server unreachable at 10.88.0.2:3307: i/o timeout")

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 0 || len(report.Reclaimed) != 0 || len(report.LandedAlready) != 0 {
		t.Fatalf("expected a landed story neither started again nor given back, got %+v", report)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "claim was kept") || !strings.Contains(report.Notes[0], "i/o timeout") {
		t.Fatalf("expected one note saying the close failed and the claim was kept, got %q", report.Notes)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Fatalf("expected the claim kept for a later tick to close, got %q held by %q", detail.Status, detail.Assignee)
	}
	if len(runner.Closed()) != 0 {
		t.Fatalf("expected the window left while the story stays claimed, got %q closed", runner.Closed())
	}

	// The next tick, once the tracker answers again, closes it.
	tracker.RefuseToClose("mw-gq6.9", "")
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the second dispatch to run cleanly, got %v", err)
	}
	if len(report.LandedAlready) != 1 || len(report.Started) != 0 {
		t.Fatalf("expected the second tick to close the landed story and start nothing, got %+v", report)
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-gq6.9"); detail.Status != apptest.StatusClosed {
		t.Fatalf("expected the landed story closed by the second tick, got %q", detail.Status)
	}
}

// ledgerSays is the vault a dispatch boots from, with the seat's ledger
// reading as given.
type ledgerSays struct {
	application.Vault
	lines []string
}

func (v ledgerSays) ReadLedger(context.Context, string) ([]string, error) { return v.lines, nil }

func TestReclaimDeadPaneClosesAStoryWhoseNewestLedgerLineLandedIt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{}) // Ahead is 0: nothing left to land
	// The run=landed write was lost, or written over by a sweep that found the
	// lease lapsed: only the landing's ledger line says it landed.
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunStuck, "lease lapsed"))
	dispatch.Boot.Vault = ledgerSays{Vault: dispatch.Boot.Vault, lines: []string{
		"| 2026-10-01 | A story (mw-gq6.9) | not landed (uncommitted-work): nothing to land | 1 | |",
		"| 2026-10-01 | A story (mw-gq6.9) | landed on main (fast-forward, b34796772efe), 1 commits | 1 | |",
	}}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Reclaimed) != 0 || len(report.Started) != 0 || len(report.LandedAlready) != 1 {
		t.Fatalf("expected the landed story closed rather than given back, got %+v", report)
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-gq6.9"); detail.Status != apptest.StatusClosed {
		t.Fatalf("expected the landed story closed, got %q", detail.Status)
	}
	if !strings.Contains(strings.Join(tracker.Comments("mw-gq6.9"), "\n"), "ledger") {
		t.Fatalf("expected the comment to say the ledger is how it was known landed, got %q", tracker.Comments("mw-gq6.9"))
	}
}

func TestReclaimDeadPaneGivesBackAStoryWhoseNewestLedgerLineDidNotLandIt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, session := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{})
	dispatch.Boot.Vault = ledgerSays{Vault: dispatch.Boot.Vault, lines: []string{
		"| 2026-09-30 | A story (mw-gq6.9) | landed on main (fast-forward, 0123456789ab), 1 commits | 1 | |",
		"| 2026-10-01 | A story (mw-gq6.9) | not landed (session-failed): the session did not finish | 1 | |",
	}}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the story to be reclaimed and redispatched, got %v", err)
	}
	assertReclaimedAsBefore(t, report, tracker, session)
}

func TestDispatchDoesNotClaimAReadyStoryWhoseLastRunLanded(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story landed and given back"})
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunLanded, "landed on main"))
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.10", Title: "A story never worked"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.10" {
		t.Fatalf("expected only mw-gq6.10 started, got %+v", report.Started)
	}
	var why string
	for _, passed := range report.Passed {
		if passed.StoryID == "mw-gq6.9" {
			why = passed.Why
		}
	}
	if !strings.Contains(why, "landed") || !strings.Contains(why, "mw next mw-gq6.9") {
		t.Fatalf("expected mw-gq6.9 passed over because its last run landed it, with the way to close it, got %q in %+v", why, report.Passed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status == apptest.StatusInProgress || detail.Assignee != "" || detail.Attempts != 0 {
		t.Fatalf("expected mw-gq6.9 left unclaimed and uncounted, got %q held by %q, %d attempts", detail.Status, detail.Assignee, detail.Attempts)
	}
	for _, name := range runner.Names() {
		if name == application.SessionName("mw-gq6.9") {
			t.Fatalf("expected no session started for mw-gq6.9, got %q", runner.Names())
		}
	}
	if !strings.Contains(report.String(), "passed  mw-gq6.9") {
		t.Fatalf("expected the printed report to say mw-gq6.9 was passed over, got:\n%s", report.String())
	}
}
