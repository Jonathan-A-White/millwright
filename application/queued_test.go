package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A story of a rig that lands in batches is queued once its close-out passes
// (plans/0117, Land Rush 2): its claim is kept, its session is closed and its
// lease lapses, and it waits, run=queued, for its batch. None of that is a
// sign of trouble, so nothing that watches claims may take it for one: the
// sweep does not mark it stuck, a dispatch neither gives its claim back nor
// counts it against the cap, a close-out does not call it abandoned, the
// quiet alarm does not name it, and mw status does not count it as a session.

// queue records id as queued for batch landing, the way mw next leaves it.
func queue(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	if err := tracker.SetStoryState(context.Background(), id, application.RunState, application.RunQueued, "queued for batch landing"); err != nil {
		t.Fatalf("queueing %s: %v", id, err)
	}
}

func TestSweepLeavesAQueuedStoryWhoseLeaseLapsedAlone(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	claimedStory(t, tracker, "mw-gq6.9", 1)
	queue(t, tracker, "mw-gq6.9")
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.9", now.Add(-time.Hour)))

	report, err := application.Sweep{Tracker: tracker, Host: "vps", Now: func() time.Time { return now }}.Run(ctx)
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	if len(report.Stuck) != 0 {
		t.Fatalf("expected no story marked stuck, got %+v", report.Stuck)
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunQueued {
		t.Fatalf("expected mw-gq6.9 still %s=%s, got %q", application.RunState, application.RunQueued, state)
	}
	if comments := tracker.Comments("mw-gq6.9"); len(comments) != 0 {
		t.Fatalf("expected no comment on a queued story, got %q", comments)
	}
	if len(report.Queued) != 1 || report.Queued[0] != "mw-gq6.9" {
		t.Fatalf("expected the sweep to say mw-gq6.9 is queued, got %+v", report)
	}
}

// aQueuedStoryAndOneReady is a dispatch with a cap of one, a story claimed here
// and queued whose window has died and whose lease ran out half an hour ago,
// and a second story ready to be worked.
func aQueuedStoryAndOneReady(t *testing.T) (application.Dispatch, *apptest.FakeTracker, *apptest.FakeRunner) {
	t.Helper()
	dispatch, tracker, runner, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{})
	queue(t, tracker, "mw-gq6.9")
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.10", Title: "The next story"})
	return dispatch, tracker, runner
}

func TestDispatchNeitherCountsNorGivesBackAQueuedStory(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, runner := aQueuedStoryAndOneReady(t)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if report.Running != 0 {
		t.Fatalf("expected the queued story not counted as a running session, got %d running", report.Running)
	}
	if len(report.Reclaimed) != 0 || len(report.LandedAlready) != 0 || len(report.HeldRefused) != 0 {
		t.Fatalf("expected the queued story neither reclaimed, closed nor held as refused, got %+v", report)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.10" {
		t.Fatalf("expected the ready story started in the slot the queued one does not hold, got %+v", report.Started)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Fatalf("expected the queued story's claim kept, got %q held by %q", detail.Status, detail.Assignee)
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunQueued {
		t.Fatalf("expected mw-gq6.9 still %s=%s, got %q", application.RunState, application.RunQueued, state)
	}
	if closed := runner.Closed(); len(closed) != 0 {
		t.Fatalf("expected no window touched, got %q closed", closed)
	}
	if len(report.Queued) != 1 || report.Queued[0] != "mw-gq6.9" {
		t.Fatalf("expected the report to name mw-gq6.9 as queued, got %+v", report.Queued)
	}
	if printed := report.String(); !strings.Contains(printed, "mw-gq6.9") || !strings.Contains(printed, "queued for batch landing") {
		t.Fatalf("expected the printed report to say mw-gq6.9 is queued, got:\n%s", printed)
	}
}

func TestADryRunDispatchDoesNotCountAQueuedStoryEither(t *testing.T) {
	dispatch, _, _ := aQueuedStoryAndOneReady(t)
	dispatch.DryRun = true

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if report.Running != 0 || len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.10" {
		t.Fatalf("expected the dry run to count nothing running and take mw-gq6.10, got running %d, started %+v", report.Running, report.Started)
	}
}

func TestNextDoesNotCallAQueuedStoryAbandoned(t *testing.T) {
	ctx := context.Background()
	lands := &fakeRetryLanding{}
	next, tracker := aNextWithAStrandedClaim(t, 2*time.Hour, lands, lands, aTranscript(""))
	queue(t, tracker, "mw-x.2")

	report, err := next.Run(ctx, "mw-x.1")
	if err != nil {
		t.Fatalf("mw next: %v", err)
	}
	if len(report.Abandoned) != 0 || len(report.GaveBack) != 0 {
		t.Fatalf("expected the queued story neither abandoned nor given back, got abandoned %q, gave back %q", report.Abandoned, report.GaveBack)
	}
	if state, _ := tracker.StoryState(ctx, "mw-x.2", application.RunState); state != application.RunQueued {
		t.Fatalf("expected mw-x.2 still %s=%s, got %q", application.RunState, application.RunQueued, state)
	}
	detail, err := tracker.ShowStory(ctx, "mw-x.2")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the queued story's claim kept, got %q", detail.Status)
	}
}

func TestNudgeSaysNothingOfAQueuedStory(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story waiting for its batch", "vps")
	claim(t, tracker, "mw-gq6.30", 3*time.Hour)
	queue(t, tracker, "mw-gq6.30")

	if clauses := nudgeReport(t, tracker, application.Nudge{}); len(clauses) != 0 {
		t.Fatalf("expected the quiet alarm to say nothing of a queued story, got %+v", clauses)
	}
}

func TestStatusShowsAQueuedStoryAsQueuedAndNotAsASession(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.31", "A story waiting for its batch", "vps")
	claim(t, tracker, "mw-gq6.31", 3*time.Hour)
	queue(t, tracker, "mw-gq6.31")

	report, err := application.Status{
		Tracker: tracker, Host: "vps", Seat: "builder", Cap: 2,
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if report.InUse != 0 {
		t.Fatalf("expected a queued story to hold none of the cap's sessions, got %d in use", report.InUse)
	}
	if len(report.Running) != 1 || !report.Running[0].Queued() || report.Running[0].Stopped() {
		t.Fatalf("expected mw-gq6.31 listed as queued and not as stopped, got %+v", report.Running)
	}
	printed := report.String()
	if !strings.Contains(printed, "queued for batch landing") || strings.Contains(printed, "NOT RUNNING") {
		t.Fatalf("expected status to show the story queued, not as a session gone, got:\n%s", printed)
	}
}

func TestCheckedTipReadsOnlyThePassedVerdictThatEndsTheLog(t *testing.T) {
	const tip = "c811cebe37590123456789abcdef0123456789ab"
	passed := application.CheckReport{StoryID: "mw-gq6.1", Branch: "mw/mw-gq6.1", Target: "main", Commits: 1, Tip: tip}.String()
	for name, held := range map[string]struct {
		log, want string
	}{
		"a check that passed at a known tip":   {"the tests ran\n" + passed, tip},
		"a check that passed at no known tip":  {application.CheckReport{Branch: "mw/mw-gq6.1", Commits: 1}.String(), ""},
		"a check that failed":                  {application.CheckReport{Branch: "mw/mw-gq6.1", Tip: tip, Refusals: []application.Refusal{{Why: "red"}}}.String(), ""},
		"a check cut off after its verdict":    {passed + "a line printed after it\n", ""},
		"a check started and not yet finished": {"check of mw-gq6.1 started in the background\n", ""},
		"an empty log":                         {"", ""},
	} {
		if got := application.CheckedTip(held.log); got != held.want {
			t.Errorf("%s: expected %q, got %q from:\n%s", name, held.want, got, held.log)
		}
	}
}
