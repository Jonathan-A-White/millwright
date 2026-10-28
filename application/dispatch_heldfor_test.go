package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A story the Mayor parked behind an event (held-for-jonah) is open and ready
// all the same, since the turn-cap hand-back reopened it: the next tick must
// not take it again, and takes it once the label is gone (mw-gq6.753).
func TestDispatchDoesNotClaimAReadyStoryHeldForAnEvent(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story parked for tomorrow"})
	mustDo(t, tracker.SetLabels("mw-gq6.9", "held-for-jonah"))
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
		if passed.StoryID == "mw-gq6.9" && passed.Gate == "held-for" {
			why = passed.Why
		}
	}
	if !strings.Contains(why, "held-for-jonah") {
		t.Fatalf("expected mw-gq6.9 passed over by the held-for gate, naming the label, got %q in %+v", why, report.Passed)
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
}

func TestDispatchClaimsAStoryOnceItsHeldForLabelIsGone(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story that was parked"})
	mustDo(t, tracker.SetLabels("mw-gq6.9", "held-for-jonah"))
	mustDo(t, tracker.SetLabels("mw-gq6.9"))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected mw-gq6.9 started once released, got %+v", report.Started)
	}
}
