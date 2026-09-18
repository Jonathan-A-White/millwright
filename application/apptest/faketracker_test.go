package apptest_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func epicDefaults() domain.Path {
	return domain.Path{
		Rig:     "millwright",
		Branch:  "main",
		Harness: domain.HarnessClaude,
		Model:   domain.ModelOpus,
		Effort:  domain.EffortHigh,
		Formula: "tdd-feature",
		Host:    "vps",
	}
}

func trackerWithOneStory(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	f := apptest.NewFakeTracker()
	f.AddEpic("mw-gq6", epicDefaults())
	f.AddStory("mw-gq6", domain.Story{
		ID:        "mw-gq6.3",
		Title:     "Beads gateway",
		Overrides: domain.Path{Model: domain.ModelHaiku},
	})
	return f
}

func TestShowStoryOverlaysTheEpicDefaults(t *testing.T) {
	f := trackerWithOneStory(t)

	detail, err := f.ShowStory(context.Background(), "mw-gq6.3")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	path, err := detail.Path()
	if err != nil {
		t.Fatalf("the story has no path: %v", err)
	}
	if path.Model != domain.ModelHaiku {
		t.Errorf("expected the story's model override to win, got %q", path.Model)
	}
	if path.Rig != "millwright" || path.Host != "vps" {
		t.Errorf("expected the epic's defaults to fall through, got %+v", path)
	}
}

func TestShowStoryReportsAnUnknownStory(t *testing.T) {
	f := apptest.NewFakeTracker()
	if _, err := f.ShowStory(context.Background(), "mw-nope"); err == nil {
		t.Fatal("expected an unknown story to be reported")
	}
}

func TestClaimingTwiceIsHarmlessButAnotherActorIsRefused(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()

	if err := f.ClaimStory(ctx, "mw-gq6.3"); err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if err := f.ClaimStory(ctx, "mw-gq6.3"); err != nil {
		t.Fatalf("expected claiming twice to be harmless, got %v", err)
	}

	detail, err := f.ShowStory(ctx, "mw-gq6.3")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee != apptest.Actor {
		t.Fatalf("expected a claimed story, got status %q assignee %q", detail.Status, detail.Assignee)
	}
}

func TestSetStoryMetadataChangesThePath(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()

	if err := f.SetStoryMetadata(ctx, "mw-gq6.3", map[string]string{"effort": "max", "run": "dispatched"}); err != nil {
		t.Fatalf("setting metadata: %v", err)
	}

	detail, err := f.ShowStory(ctx, "mw-gq6.3")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	path, err := detail.Path()
	if err != nil {
		t.Fatalf("the story has no path: %v", err)
	}
	if path.Effort != domain.EffortMax {
		t.Errorf("expected the written effort to be read back, got %q", path.Effort)
	}
	if f.Metadata("mw-gq6.3")["run"] != "dispatched" {
		t.Errorf("expected metadata that is not a path field to be kept, got %v", f.Metadata("mw-gq6.3"))
	}
}

func TestCommentAndCloseAreRecorded(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()

	if err := f.CommentOnStory(ctx, "mw-gq6.3", "green on the first go"); err != nil {
		t.Fatalf("commenting: %v", err)
	}
	if err := f.CloseStory(ctx, "mw-gq6.3", "merged"); err != nil {
		t.Fatalf("closing: %v", err)
	}

	if got := f.Comments("mw-gq6.3"); len(got) != 1 || got[0] != "green on the first go" {
		t.Errorf("expected the comment to be kept, got %v", got)
	}
	if got := f.CloseReason("mw-gq6.3"); got != "merged" {
		t.Errorf("expected the close reason to be kept, got %q", got)
	}
}

func TestStaleClaimsFindsOnlyClaimsLeftTooLong(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()

	if err := f.ClaimStory(ctx, "mw-gq6.3"); err != nil {
		t.Fatalf("claiming: %v", err)
	}

	stale, err := f.StaleClaims(ctx, 2)
	if err != nil {
		t.Fatalf("listing stale claims: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected a fresh claim not to be stale, got %v", apptest.IDs(stale))
	}

	f.Touched("mw-gq6.3", time.Now().AddDate(0, 0, -5))
	stale, err = f.StaleClaims(ctx, 2)
	if err != nil {
		t.Fatalf("listing stale claims: %v", err)
	}
	if got := apptest.IDs(stale); len(got) != 1 || got[0] != "mw-gq6.3" {
		t.Fatalf("expected the abandoned claim, got %v", got)
	}
}
