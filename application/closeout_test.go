package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestNudgeSaysNothingOfAStoryWhoseCloseOutIsRunning(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story being landed", "vps")
	claim(t, tracker, "mw-gq6.30", 108*time.Minute)
	marks := apptest.NewFakeCloseOutMarks()
	_ = marks.Write(context.Background(), application.CloseOutMark{Story: "mw-gq6.30", Since: statusNow.Add(-21 * time.Minute), Calming: true})

	clauses := nudgeReport(t, tracker, application.Nudge{CloseOuts: marks})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a story being closed out, got %+v", clauses)
	}
}

func TestNudgeStillNamesAStoryWhoseSessionEndedWithNoCloseOutRunning(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story nobody is closing", "vps")
	storyOn(t, tracker, "mw-gq6.31", "A story being landed", "vps")
	claim(t, tracker, "mw-gq6.30", 108*time.Minute)
	claim(t, tracker, "mw-gq6.31", 108*time.Minute)
	marks := apptest.NewFakeCloseOutMarks()
	_ = marks.Write(context.Background(), application.CloseOutMark{Story: "mw-gq6.31", Since: statusNow.Add(-5 * time.Minute)})

	clauses := nudgeReport(t, tracker, application.Nudge{CloseOuts: marks})

	if len(clauses) != 1 || clauses[0].Key != "mw-gq6.30" {
		t.Fatalf("expected only the story with no close-out named, got %+v", clauses)
	}
}

func TestStatusShowsHowLongAStoryHasBeenClosingOutAndThatItWaitsForCalm(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story being landed", "vps")
	storyOn(t, tracker, "mw-gq6.31", "A story waiting for calm", "vps")
	claim(t, tracker, "mw-gq6.30", 108*time.Minute)
	claim(t, tracker, "mw-gq6.31", 108*time.Minute)
	marks := apptest.NewFakeCloseOutMarks()
	_ = marks.Write(context.Background(), application.CloseOutMark{Story: "mw-gq6.30", Since: statusNow.Add(-21 * time.Minute)})
	_ = marks.Write(context.Background(), application.CloseOutMark{Story: "mw-gq6.31", Since: statusNow.Add(-4 * time.Minute), Calming: true})

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		CloseOuts: marks, Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()

	for _, want := range []string{"closing out 21m\n", "closing out 4m, waiting for calm"} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected status to say %q, got:\n%s", want, printed)
		}
	}
}

func TestStatusSaysNothingOfClosingOutForAStoryWithNoCloseOutRunning(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story", "vps")
	claim(t, tracker, "mw-gq6.30", 108*time.Minute)

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		CloseOuts: apptest.NewFakeCloseOutMarks(), Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if strings.Contains(report.String(), "closing out") {
		t.Fatalf("expected no closing-out line, got:\n%s", report.String())
	}
}

func TestACloseOutMarksItsStoryWhileItRunsAndWhileItWaitsForCalmAndClearsItAtTheEnd(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	marks := apptest.NewFakeCloseOutMarks()
	// While the close-out waits for calm, the mark must say so.
	var whileWaiting []application.CloseOutMark
	_, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		Load:      &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}},
		LoadPoll:  30 * time.Second,
		LoadBound: 15 * time.Minute,
		LoadWait: func(context.Context, time.Duration) error {
			whileWaiting, _ = marks.Read(ctx)
			return nil
		},
		CloseOuts: marks,
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if err != nil {
		t.Fatalf("closing out: %v", err)
	}

	if len(whileWaiting) != 1 || whileWaiting[0].Story != "mw-x.1" || !whileWaiting[0].Calming {
		t.Errorf("expected a mark saying the close-out waits for calm while it waited, got %+v", whileWaiting)
	}
	if len(marks.Writes) < 3 || marks.Writes[0].Calming || marks.Writes[len(marks.Writes)-1].Calming {
		t.Errorf("expected the close-out marked running, then calming, then running again, got %+v", marks.Writes)
	}
	if left, _ := marks.Read(ctx); len(left) != 0 {
		t.Errorf("expected the mark cleared when the close-out ended, got %+v", left)
	}
}
