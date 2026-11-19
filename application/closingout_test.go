package application_test

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1498: a close-out run by hand (mw next --no-dispatch --met-by-mayor)
// that waits for the gate lock leaves its mark on the story in the tracker, so
// that no host's dispatch takes the story again and spends an attempt.
func TestADispatchDoesNotTakeAStoryWhoseCloseOutIsRunning(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.Now = func() time.Time { return now }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Refused for open steps"})
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.AttemptsField: "2"}))
	mark := application.ClosingOut{By: "laptop", Since: now.Add(-45 * time.Minute), Until: now.Add(4 * time.Minute)}
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.ClosingOutField: mark.String()}))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected nothing started for a story under a running close-out, got %+v", report.Started)
	}
	if len(report.Passed) != 1 || report.Passed[0].Gate != "closing-out" ||
		!strings.Contains(report.Passed[0].Why, "landed by the Mayor") || !strings.Contains(report.Passed[0].Why, "laptop") {
		t.Fatalf("expected it passed over at the closing-out gate, naming the Mayor's landing, got %+v", report.Passed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Attempts != 2 || detail.Status != application.StatusOpen {
		t.Fatalf("expected no attempt spent and no claim made, got attempts=%d status=%q", detail.Attempts, detail.Status)
	}

	// Its close-out ended: the mark is cleared, and the story is offered again.
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.ClosingOutField: ""}))
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching again: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.1" {
		t.Fatalf("expected the story started once its mark was cleared, got %+v (passed %+v)", report.Started, report.Passed)
	}
}

func TestADispatchTakesAStoryAgainOnceTheLeaseOfItsCloseOutLapses(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Now = func() time.Time { return now }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Its close-out died"})
	mark := application.ClosingOut{By: "laptop", Since: now.Add(-50 * time.Minute), Until: now.Add(-time.Minute)}
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.ClosingOutField: mark.String()}))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("expected a mark whose lease lapsed to hold nothing, got started %+v passed %+v", report.Started, report.Passed)
	}
}

// The close-out that left the mark may itself dispatch (a story it gave back
// open is started afresh by it), so its own mark does not hold it.
func TestTheCloseOutThatLeftTheMarkIsNotHeldByItsOwnMark(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Now = func() time.Time { return now }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Given back open"})
	mark := application.ClosingOut{By: "vps", Since: now.Add(-time.Minute), Until: now.Add(4 * time.Minute)}
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.ClosingOutField: mark.String()}))

	report, err := dispatch.Run(application.WithCloseOutOf(ctx, "mw-gq6.1"))
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("expected the close-out's own dispatch to start the story, got %+v", report.Started)
	}
}

// A claim kept by a story whose close-out is running is not reclaimed as a dead
// pane (which would refund or spend an attempt), whatever its lease says.
func TestADispatchDoesNotReclaimAClaimWhoseCloseOutIsRunning(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Now = func() time.Time { return now }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Claimed, session over"})
	mustDo(t, tracker.ClaimStory(ctx, "mw-gq6.1"))
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.1", now.Add(-time.Minute)))
	mark := application.ClosingOut{By: "vps", Since: now.Add(-time.Hour), Until: now.Add(4 * time.Minute)}
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{application.ClosingOutField: mark.String()}))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Reclaimed) != 0 || len(report.Started) != 0 {
		t.Fatalf("expected the claim left alone, got reclaimed %+v started %+v", report.Reclaimed, report.Started)
	}
}

// mw-gq6.1498 AC4. mw-gq6.1490 stood at attempts=5 with a fifth session
// started: the cap counts a story's own attempts (OwnAttempts), and four of its
// five were named tooling (mw cause), which spend no attempt of the story's.
// A story whose own attempts stand at max_attempts is not started again, and a
// later attempt named tooling does not lift that.
func TestAStoryWhoseOwnAttemptsStandAtTheCapIsNotStartedAgainForALaterToolingAttempt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.MaxAttempts = 3
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Three attempts of its own"})
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{
		application.AttemptsField: strconv.Itoa(4), application.AttemptCausesField: "4:tooling",
	}))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected a story at its cap not started, got %+v", report.Started)
	}
	if len(report.Passed) != 1 || report.Passed[0].Gate != "attempts-exhausted" {
		t.Fatalf("expected it passed over at attempts-exhausted, got %+v", report.Passed)
	}
}

func TestAStoryWhoseEarlierAttemptsWereToolingStillGetsAnotherAttempt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.MaxAttempts = 3
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Four tooling attempts, one its own"})
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{
		application.AttemptsField: strconv.Itoa(5), application.AttemptCausesField: "1:tooling,2:tooling,4:tooling,5:tooling",
	}))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("expected the tooling attempts uncounted by design (OwnAttempts), got %+v passed %+v", report.Started, report.Passed)
	}
}

func TestAClosingOutMarkIsReadBackAsWritten(t *testing.T) {
	at := time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)
	mark := application.ClosingOut{By: "laptop", Since: at, Until: at.Add(5 * time.Minute)}
	got, ok := application.ParseClosingOut(mark.String())
	if !ok || !got.Since.Equal(mark.Since) || !got.Until.Equal(mark.Until) || got.By != "laptop" {
		t.Fatalf("expected %+v read back, got %+v (%v)", mark, got, ok)
	}
	if _, ok := application.ParseClosingOut(""); ok {
		t.Fatal("expected no mark from nothing")
	}
	if _, ok := application.ParseClosingOut("laptop soon later"); ok {
		t.Fatal("expected no mark from words that are not times")
	}
}

// mw-gq6.1498 AC3: mw status words a story held by the tracker's mark as it
// words one held by this host's own file.
func TestStatusSaysAReadyStoryIsClosingOutFromTheTrackersMark(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Given back open, being landed", "vps")
	mark := application.ClosingOut{By: "laptop", Since: statusNow.Add(-21 * time.Minute), Until: statusNow.Add(3 * time.Minute)}
	mustDo(t, tracker.SetStoryMetadata(context.Background(), "mw-gq6.30", map[string]string{application.ClosingOutField: mark.String()}))

	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", Now: func() time.Time { return statusNow }}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if !strings.Contains(report.String(), "closing out 21m") {
		t.Fatalf("expected status to say the story is closing out 21m, got:\n%s", report.String())
	}
}

// aCheckThatLooksAtTheMark is a rig's tests that read the story's tracker mark
// while the close-out runs.
type aCheckThatLooksAtTheMark struct {
	tracker *apptest.FakeTracker
	seen    application.ClosingOut
}

func (c *aCheckThatLooksAtTheMark) Run(ctx context.Context, _, _ string) (application.Checked, error) {
	detail, _ := c.tracker.ShowStory(ctx, "mw-x.1")
	c.seen = detail.ClosingOut
	return application.Checked{Passed: true}, nil
}

// mw-gq6.1498: Next.Run (by hand with --no-dispatch, --met-by-mayor and
// --rescoped-by-mayor alike, and the unit contrib/mw-closeout starts, all run
// it) marks the story in the tracker while it runs and clears the mark at the end.
func TestACloseOutMarksItsStoryInTheTrackerWhileItRunsAndClearsItAtTheEnd(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	checks := &aCheckThatLooksAtTheMark{tracker: tracker}
	next := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checks, Slot: aSlot{}, Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, Out: io.Discard, Err: io.Discard,
	}

	report, err := next.Run(ctx, "mw-x.1")
	if err != nil || !report.Landed {
		t.Fatalf("expected a landing, got %v and %+v", err, report)
	}

	if checks.seen.By != "laptop" || !checks.seen.Live(time.Now()) {
		t.Errorf("expected the story to carry laptop's live mark while the tests ran, got %+v", checks.seen)
	}
	detail, err := tracker.ShowStory(ctx, "mw-x.1")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.ClosingOut.Until.IsZero() {
		t.Errorf("expected the mark cleared at the end, got %+v", detail.ClosingOut)
	}
}
