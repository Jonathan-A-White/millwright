package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// mw-gq6.244: a story whose title is under the tracker's limit can still have
// a step the tracker refuses at the pour, and the refusal is the story's, not
// the run's: it is left claimed and blocked with the reason on it, so that the
// next dispatch passes it over instead of giving it back and springing again.
func TestDispatchBlocksAStoryWhoseFormulaThePourRefuses(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	dispatch.Cap = 2
	log := &apptest.FakeEventLog{}
	dispatch.Events = log
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story with a long title"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Another story"})
	refusal := &application.PourRefused{Formula: "tdd-feature", Story: "mw-gq6.1", Reason: "title must be 500 characters or less (got 512)"}
	tracker.FailOn("PourFormula", refusal)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the run not to fail for one story's refused pour, got %v", err)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("expected no failure counted against the run, got %+v", report.Failed)
	}
	var passed bool
	for _, p := range report.Passed {
		if p.StoryID == "mw-gq6.1" && strings.Contains(p.Why, "got 512") {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("expected mw-gq6.1 passed over with the reason, got %+v", report.Passed)
	}

	if run := tracker.State("mw-gq6.1", application.RunState); run != application.RunBlocked {
		t.Errorf("run state = %q, want %q", run, application.RunBlocked)
	}
	if reason := tracker.StateReason("mw-gq6.1", application.RunState); !strings.Contains(reason, "got 512") {
		t.Errorf("expected the run state's reason to carry the refusal, got %q", reason)
	}
	if comments := strings.Join(tracker.Comments("mw-gq6.1"), "\n"); !strings.Contains(comments, "got 512") || !strings.Contains(comments, "tdd-feature") {
		t.Errorf("expected a comment with the formula and the refusal, got %q", comments)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Errorf("expected the claim kept, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	if _, removed := worktrees.was(); len(removed) == 0 {
		t.Errorf("expected the worktree cut for the story removed, as nothing is running in it")
	}

	var told bool
	for _, e := range log.All() {
		if e.Kind == events.KindJob && e.To == events.JobFailed && strings.Contains(e.Detail, "mw-gq6.1") && strings.Contains(e.Detail, "got 512") {
			told = true
		}
	}
	if !told {
		t.Errorf("expected a failed job event naming the story and why, got %+v", log.All())
	}

	// The next dispatch holds it, and does not pour for it again.
	poured := countAsked(tracker, "PourFormula")
	before := len(runner.Names())
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if countAsked(tracker, "PourFormula") > poured+1 {
		t.Errorf("expected at most the other story's pour on the second run, asked %d then %d", poured, countAsked(tracker, "PourFormula"))
	}
	var held bool
	for _, h := range report.HeldRefused {
		if h.StoryID == "mw-gq6.1" {
			held = true
		}
	}
	if !held {
		t.Errorf("expected the second dispatch to hold mw-gq6.1 as refused, got %+v", report.HeldRefused)
	}
	if len(runner.Names()) < before {
		t.Errorf("sessions went down from %d", before)
	}
}

func countAsked(tracker *apptest.FakeTracker, method string) int {
	var n int
	for _, asked := range tracker.Asked() {
		if asked == method {
			n++
		}
	}
	return n
}
