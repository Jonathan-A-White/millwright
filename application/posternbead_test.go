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

// beadTracker is an epic with a story that waits on one sibling and is waited
// on by another, and a child epic.
func beadTracker(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-a", domain.Path{Rig: "postern", Branch: "main", Host: "desktop", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Harness: "claude"})
	tracker.DescribeEpic("mw-a", "The epic", apptest.StatusOpen, 1)
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "First"})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "Pick the storage engine", Overrides: domain.Path{Model: "opus"}})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.4", Title: "After"})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.5", Title: "Also after, but done"})
	tracker.AddChildEpic("mw-a", "mw-a.6", "A child epic")
	tracker.Needs("mw-a.3", "mw-a.2")
	tracker.Needs("mw-a.4", "mw-a.3")
	tracker.Needs("mw-a.5", "mw-a.3")
	mustDo(t, tracker.SetStatus("mw-a.5", apptest.StatusClosed))
	mustDo(t, tracker.SetDescription("mw-a.3", "## Why\n\nBecause."))
	mustDo(t, tracker.SetLabels("mw-a.3", "hitl"))
	mustDo(t, tracker.SetCreated("mw-a.3", time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)))
	mustDo(t, tracker.CommentOnStoryAt("mw-a.3", "first word", time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)))
	mustDo(t, tracker.CommentOnStoryAt("mw-a.3", strings.Repeat("y", 17000), time.Date(2026, 9, 27, 11, 0, 0, 0, time.UTC)))
	return tracker
}

// A story's detail is §12's shape: every field the view carries, what it
// waits on and what waits on it, its full description and every comment,
// oldest first, each cut at 16000 runes.
func TestPosternBeadDetailsAStory(t *testing.T) {
	tracker := beadTracker(t)

	detail, err := application.PosternBead{Tracker: tracker}.Build(context.Background(), "mw-a.3")
	if err != nil {
		t.Fatalf("building the detail: %v", err)
	}
	if detail.V != 2 || detail.ID != "mw-a.3" || detail.Title != "Pick the storage engine" || detail.Type != "task" ||
		detail.Status != "open" || detail.Parent != "mw-a" || detail.Created != "2026-09-27T09:00:00Z" {
		t.Errorf("expected the story's own fields, got %+v", detail)
	}
	if !equalStrings(detail.Waits, []string{"mw-a.2"}) || !equalStrings(detail.Blocks, []string{"mw-a.4"}) {
		t.Errorf("expected it to wait on mw-a.2 and block mw-a.4 (mw-a.5 is done), got waits %v blocks %v", detail.Waits, detail.Blocks)
	}
	if detail.Children == nil || len(detail.Children) != 0 {
		t.Errorf("expected an empty children array, got %#v", detail.Children)
	}
	if detail.Path == nil || detail.Path.Model != "opus" || detail.Path.Rig != "postern" || detail.Path.Formula != "tdd-feature" {
		t.Errorf("expected the merged path, got %+v", detail.Path)
	}
	if detail.Description != "## Why\n\nBecause." || !equalStrings(detail.Labels, []string{"hitl"}) {
		t.Errorf("expected the full markdown description and the label, got %q %v", detail.Description, detail.Labels)
	}
	if len(detail.Comments) != 2 || detail.Comments[0].Text != "first word" || detail.Comments[0].At != "2026-09-27T10:00:00Z" ||
		detail.Comments[0].Author == "" {
		t.Fatalf("expected both comments, oldest first, with time and author, got %+v", detail.Comments)
	}
	if n := len([]rune(detail.Comments[1].Text)); n != application.PosternBeadCommentLimit {
		t.Errorf("expected the long comment cut to %d runes, got %d", application.PosternBeadCommentLimit, n)
	}
}

// An epic's detail names its children and carries its own defaults as its
// path.
func TestPosternBeadDetailsAnEpic(t *testing.T) {
	tracker := beadTracker(t)

	detail, err := application.PosternBead{Tracker: tracker}.Build(context.Background(), "mw-a")
	if err != nil {
		t.Fatalf("building the detail: %v", err)
	}
	if detail.Type != "epic" || detail.Priority != 1 || detail.Parent != "" {
		t.Errorf("expected the root epic, got %+v", detail)
	}
	if !equalStrings(detail.Children, []string{"mw-a.2", "mw-a.3", "mw-a.4", "mw-a.5", "mw-a.6"}) {
		t.Errorf("expected every child, in filed order, got %v", detail.Children)
	}
	if detail.Path == nil || detail.Path.Harness != "claude" {
		t.Errorf("expected the epic's defaults as its path, got %+v", detail.Path)
	}
}

// A bead the tracker does not know is reported as missing, which mw postern
// bead leaves with exit status 3 (§12's 404) — and so is an id that could
// never name one.
func TestPosternBeadReportsAnUnknownBeadAsMissing(t *testing.T) {
	tracker := beadTracker(t)
	for _, id := range []string{"mw-nope", "--all", ""} {
		_, err := application.PosternBead{Tracker: tracker}.Build(context.Background(), id)
		if !application.PosternBeadIsMissing(err) {
			t.Fatalf("expected %q to be reported missing, got %v", id, err)
		}
		if application.ExitStatus(err) == 0 {
			t.Fatalf("expected a missing bead to be a failure")
		}
	}
}

// Run seals the detail exactly as the view is sealed, and prints it.
func TestPosternBeadRunPrintsTheSealedDetail(t *testing.T) {
	tracker := beadTracker(t)
	cipher := apptest.NewFakeCipher()
	var out strings.Builder

	if _, err := (application.PosternBead{Tracker: tracker, Cipher: cipher, GovernorKey: "governor-pubkey-hex", Out: &out}).Run(context.Background(), "mw-a.3"); err != nil {
		t.Fatalf("running: %v", err)
	}
	var opened application.PosternBeadDetail
	if err := application.OpenPosternDoc(cipher, "any", out.String(), &opened); err != nil {
		t.Fatalf("opening what was printed: %v", err)
	}
	if opened.ID != "mw-a.3" || len(opened.Comments) != 2 {
		t.Fatalf("expected the sealed detail of mw-a.3, got %+v", opened)
	}
}

// Detail is read-only: nothing is written to the tracker.
func TestPosternBeadWritesNothing(t *testing.T) {
	tracker := beadTracker(t)
	before := tracker.Writes()
	if _, err := (application.PosternBead{Tracker: tracker}).Build(context.Background(), "mw-a.3"); err != nil {
		t.Fatal(err)
	}
	if tracker.Writes() != before {
		t.Fatalf("expected no write, got %d", tracker.Writes()-before)
	}
}
