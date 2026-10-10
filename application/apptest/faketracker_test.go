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

// ShowBeads reads stories and epics alike, in the order asked, leaving out an
// id the fake does not hold, the way bd show skips one it does not know: a
// root epic, a child epic and a story each come back as one bead.
func TestShowBeadsReadsStoriesAndEpicsAndSkipsTheUnknown(t *testing.T) {
	f := trackerWithOneStory(t)
	f.DescribeEpic("mw-gq6", "Walking skeleton", apptest.StatusInProgress, 1)
	f.AddChildEpic("mw-gq6", "mw-gq6.9", "A child epic")
	ctx := context.Background()

	found, err := f.ShowBeads(ctx, []string{"mw-gq6.3", "mw-nope", "mw-gq6", "mw-gq6.9"})
	if err != nil {
		t.Fatalf("showing the beads: %v", err)
	}
	if got := apptest.IDs(found); len(got) != 3 || got[0] != "mw-gq6.3" || got[1] != "mw-gq6" || got[2] != "mw-gq6.9" {
		t.Fatalf("expected mw-gq6.3, mw-gq6, mw-gq6.9 in that order, got %v", got)
	}
	if found[0].IsEpic || found[0].EpicID != "mw-gq6" {
		t.Errorf("expected mw-gq6.3 to be a story under mw-gq6, got %+v", found[0])
	}
	root := found[1]
	if !root.IsEpic || root.Story.Title != "Walking skeleton" || root.Status != apptest.StatusInProgress || root.Priority != 1 || root.EpicID != "" {
		t.Errorf("expected the root epic as described, with no parent, got %+v", root)
	}
	if !found[2].IsEpic || found[2].EpicID != "mw-gq6" {
		t.Errorf("expected the child epic filed under mw-gq6, got %+v", found[2])
	}
}

// ShowBeads narrows a bead's Needs to what is unfinished, as bd show's
// whole linked beads let it.
func TestShowBeadsNarrowsNeedsToTheUnfinished(t *testing.T) {
	f := trackerWithOneStory(t)
	f.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Done"})
	f.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Not yet"})
	if err := f.SetStatus("mw-gq6.1", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	f.Needs("mw-gq6.3", "mw-gq6.1", "mw-gq6.2")

	found, err := f.ShowBeads(context.Background(), []string{"mw-gq6.3"})
	if err != nil {
		t.Fatalf("showing: %v", err)
	}
	if len(found) != 1 || len(found[0].Needs) != 1 || found[0].Needs[0] != "mw-gq6.2" {
		t.Fatalf("expected mw-gq6.3 to wait only on mw-gq6.2, got %+v", found)
	}
}

// An epic's own bead rides along with ShowEpic: its parent, labels and
// comment count, as a listing of its parent's children would report it.
func TestShowEpicCarriesTheEpicsOwnBead(t *testing.T) {
	f := trackerWithOneStory(t)
	f.AddChildEpic("mw-gq6", "mw-gq6.9", "A child epic")
	if err := f.SetLabels("mw-gq6.9", "wayfinder:map"); err != nil {
		t.Fatal(err)
	}
	f.AddEpicComment("mw-gq6", "a word on the root")

	root, err := f.ShowEpic(context.Background(), "mw-gq6")
	if err != nil {
		t.Fatalf("showing the root: %v", err)
	}
	if root.Bead.Story.ID != "mw-gq6" || !root.Bead.IsEpic || root.Bead.EpicID != "" || root.Bead.CommentCount != 1 {
		t.Errorf("expected the root's own bead, with one comment and no parent, got %+v", root.Bead)
	}
	child, err := f.ShowEpic(context.Background(), "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the child: %v", err)
	}
	if child.Bead.EpicID != "mw-gq6" || len(child.Bead.Labels) != 1 || child.Bead.Labels[0] != "wayfinder:map" {
		t.Errorf("expected the child epic's parent and label, got %+v", child.Bead)
	}
}

func TestSetStoryPriorityAndHoldStoryWriteTheStory(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()

	if err := f.SetStoryPriority(ctx, "mw-gq6.3", 0); err != nil {
		t.Fatalf("setting the priority: %v", err)
	}
	if err := f.HoldStory(ctx, "mw-gq6.3"); err != nil {
		t.Fatalf("holding: %v", err)
	}
	detail, err := f.ShowStory(ctx, "mw-gq6.3")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Priority != 0 || detail.Status != apptest.StatusDeferred {
		t.Fatalf("expected priority 0 and held, got priority %d, status %s", detail.Priority, detail.Status)
	}
	if err := f.SetStoryPriority(ctx, "mw-gq6", 4); err != nil {
		t.Fatalf("setting the epic's priority: %v", err)
	}
	epic, err := f.ShowEpic(ctx, "mw-gq6")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Priority != 4 {
		t.Fatalf("expected the epic at priority 4, got %d", epic.Priority)
	}
	if err := f.SetStoryPriority(ctx, "mw-gq6.3", 5); err == nil {
		t.Fatal("expected priority 5 to be refused: priorities run 0 to 4")
	}
}

func TestAddLabelPutsALabelOnOnce(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()
	if err := f.SetLabels("mw-gq6.3", "demo"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.AddLabel(ctx, "mw-gq6.3", "hitl"); err != nil {
			t.Fatalf("adding the label: %v", err)
		}
	}
	detail, err := f.ShowStory(ctx, "mw-gq6.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Labels) != 2 || detail.Labels[0] != "demo" || detail.Labels[1] != "hitl" {
		t.Fatalf("expected demo and hitl, once each, got %v", detail.Labels)
	}
	if err := f.AddLabel(ctx, "mw-nope", "hitl"); err == nil {
		t.Fatal("expected a bead the fake does not hold refused")
	}
}

func TestRemoveLabelTakesALabelOffAndLeavesTheRest(t *testing.T) {
	f := trackerWithOneStory(t)
	ctx := context.Background()
	if err := f.SetLabels("mw-gq6.3", "demo", "waiting:others"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.RemoveLabel(ctx, "mw-gq6.3", "waiting:others"); err != nil {
			t.Fatalf("removing the label: %v", err)
		}
	}
	detail, err := f.ShowStory(ctx, "mw-gq6.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Labels) != 1 || detail.Labels[0] != "demo" {
		t.Fatalf("expected only demo left, got %v", detail.Labels)
	}
	if err := f.RemoveLabel(ctx, "mw-nope", "demo"); err == nil {
		t.Fatal("expected a bead the fake does not hold refused")
	}
}
