package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A bead typed task that has child beads is a parent: its children are the
// work, and a session handed the parent itself has nothing a Builder can do
// (mw-qtu3zvz.49 was taken twice, with 35 children, and committed nothing).
// Dispatch passes it over by the parent gate and takes the next story
// (mw-gq6.1086).
func TestDispatchDoesNotClaimAReadyStoryThatHasChildren(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A task with children"})
	tracker.AddStory("mw-gq6.9", domain.Story{ID: "mw-gq6.9.1", Title: "Its first child"})
	tracker.AddStory("mw-gq6.9", domain.Story{ID: "mw-gq6.9.2", Title: "Its second child"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.10", Title: "A story with no children"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.10" {
		t.Fatalf("expected only mw-gq6.10 started, got %+v", report.Started)
	}
	var why string
	for _, passed := range report.Passed {
		if passed.StoryID == "mw-gq6.9" && passed.Gate == "parent" {
			why = passed.Why
		}
	}
	if !strings.Contains(why, "it has 2 child stories: a parent is never a session's to take") {
		t.Fatalf("expected mw-gq6.9 passed over by the parent gate, saying why, got %q in %+v", why, report.Passed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Assignee != "" || detail.Attempts != 0 {
		t.Fatalf("expected mw-gq6.9 left unclaimed and uncounted, got held by %q, %d attempts", detail.Assignee, detail.Attempts)
	}
	for _, name := range runner.Names() {
		if name == application.SessionName("mw-gq6.9") {
			t.Fatalf("expected no session started for mw-gq6.9, got %q", runner.Names())
		}
	}
}

// A parent whose children are all closed has no work left in them to wait for,
// and a lone child does not make a story a parent of one that is closed.
func TestDispatchClaimsAStoryWhoseChildrenAreAllClosed(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A task whose children are done"})
	tracker.AddStory("mw-gq6.9", domain.Story{ID: "mw-gq6.9.1", Title: "Its only child"})
	if err := tracker.CloseStory(ctx, "mw-gq6.9.1", "done"); err != nil {
		t.Fatalf("closing the child: %v", err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected mw-gq6.9 started, got %+v passed %+v", report.Started, report.Passed)
	}
}
