package apptest_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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

func TestAFiledStoryIsHeldUntilItIsReleased(t *testing.T) {
	f := apptest.NewFakeTracker()
	ctx := context.Background()

	epicID, err := f.CreateEpic(ctx, application.NewEpic{Title: "Walking skeleton", Defaults: epicDefaults()})
	if err != nil {
		t.Fatalf("filing the epic: %v", err)
	}
	first, err := f.CreateStory(ctx, application.NewStory{EpicID: epicID, Title: "Go module", Acceptance: "it passes", EstimateMinutes: 60})
	if err != nil {
		t.Fatalf("filing the first story: %v", err)
	}
	second, err := f.CreateStory(ctx, application.NewStory{
		EpicID:     epicID,
		Title:      "Beads gateway",
		Acceptance: "it passes",
		Overrides:  domain.Path{Model: domain.ModelSonnet},
		Needs:      []string{first},
	})
	if err != nil {
		t.Fatalf("filing the second story: %v", err)
	}

	// Held: nothing is ready, however complete its path is.
	ready, err := f.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("expected held stories not to be ready, got %v", apptest.IDs(ready))
	}

	// Released: the one that waits on nothing is ready, the other is not.
	for _, id := range []string{first, second} {
		if err := f.ReleaseStory(ctx, id); err != nil {
			t.Fatalf("releasing %s: %v", id, err)
		}
	}
	ready, err = f.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories after the release: %v", err)
	}
	if got := apptest.IDs(ready); len(got) != 1 || got[0] != first {
		t.Fatalf("expected only %s to be ready, got %v", first, got)
	}

	// And once what it waits on is closed, the other one is ready too.
	if err := f.CloseStory(ctx, first, "worked"); err != nil {
		t.Fatalf("closing %s: %v", first, err)
	}
	ready, err = f.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories after the close: %v", err)
	}
	if got := apptest.IDs(ready); len(got) != 1 || got[0] != second {
		t.Fatalf("expected %s to be ready once %s is closed, got %v", second, first, got)
	}

	detail, err := f.ShowStory(ctx, second)
	if err != nil {
		t.Fatalf("showing %s: %v", second, err)
	}
	if path, err := detail.Path(); err != nil || path.Model != domain.ModelSonnet {
		t.Errorf("expected the filed story to carry its path override, got %+v (%v)", path, err)
	}
}

func TestAStoryCannotBeFiledWaitingOnOneThatIsNot(t *testing.T) {
	f := apptest.NewFakeTracker()
	ctx := context.Background()

	epicID, err := f.CreateEpic(ctx, application.NewEpic{Title: "Walking skeleton", Defaults: epicDefaults()})
	if err != nil {
		t.Fatalf("filing the epic: %v", err)
	}
	if _, err := f.CreateStory(ctx, application.NewStory{EpicID: epicID, Title: "Gateway", Needs: []string{"f-1.9"}}); err == nil {
		t.Fatal("expected a story waiting on a story that is not filed to be refused")
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

func TestStaleClaimsFindsOnlyClaimsWhoseLeaseRanOut(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()
	claimed := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	f.Clock = func() time.Time { return claimed }

	if err := f.ClaimStory(ctx, "mw-gq6.3"); err != nil {
		t.Fatalf("claiming: %v", err)
	}

	stale, err := f.StaleClaims(ctx, claimed.Add(apptest.LeaseTTL))
	if err != nil {
		t.Fatalf("listing stale claims: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected a claim whose lease has not run out not to be stale, got %v", apptest.IDs(stale))
	}

	stale, err = f.StaleClaims(ctx, claimed.Add(apptest.LeaseTTL+time.Second))
	if err != nil {
		t.Fatalf("listing stale claims: %v", err)
	}
	if got := apptest.IDs(stale); len(got) != 1 || got[0] != "mw-gq6.3" {
		t.Fatalf("expected the abandoned claim, got %v", got)
	}

	if err := f.SetLeaseExpires("mw-gq6.3", time.Time{}); err != nil {
		t.Fatalf("clearing the lease: %v", err)
	}
	stale, err = f.StaleClaims(ctx, claimed.AddDate(1, 0, 0))
	if err != nil {
		t.Fatalf("listing stale claims: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected a claim with no lease never to be stale, got %v", apptest.IDs(stale))
	}
}
