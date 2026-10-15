package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// filedAndHeld files the two-story plan the way an unattended `mw file` does:
// everything in the tracker, everything held.
func filedAndHeld(t *testing.T) (*apptest.FakeTracker, application.FiledPlan) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	filed, err := application.File{Tracker: tracker}.Run(context.Background(), twoStoryPlan())
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}
	if filed.Released {
		t.Fatal("expected an unattended filing to leave the plan held")
	}
	return tracker, filed
}

func TestReleasingAFiledPlanPrintsItsTreeAndSaysWhatIsReady(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	out := &strings.Builder{}

	found, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), filed.EpicID)
	if err != nil {
		t.Fatalf("releasing %s: %v", filed.EpicID, err)
	}
	if !found.Released {
		t.Error("expected the release to report that it released the plan")
	}

	// The tree is the tree the filing printed: the epic, the default path, and
	// every story with its own path, its estimate and what it waits on.
	module, gateway := filed.Stories[0].ID, filed.Stories[1].ID
	for _, want := range []string{
		filed.EpicID + " · Walking skeleton",
		"default path millwright/main · claude/opus/high · tdd-feature · vps",
		module + " · Go module",
		"held · 60m · waits on nothing",
		gateway + " · Beads gateway",
		"held · waits on " + module,
		"Released the 2 held stories of " + filed.EpicID + ". Ready now: " + module + ".",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the release to say %q, got:\n%s", want, out.String())
		}
	}

	// And exactly the story that waits on nothing is takeable now.
	ready, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}
	if got := apptest.IDs(ready); len(got) != 1 || got[0] != module {
		t.Fatalf("expected only %s to be ready, got %v", module, got)
	}
}

func TestReleasingTwiceChangesNothing(t *testing.T) {
	tracker, filed := filedAndHeld(t)

	first := &strings.Builder{}
	if _, err := (application.Release{Tracker: tracker, Out: first}).Run(context.Background(), filed.EpicID); err != nil {
		t.Fatalf("releasing %s: %v", filed.EpicID, err)
	}
	before, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}

	second := &strings.Builder{}
	if _, err := (application.Release{Tracker: tracker, Out: second}).Run(context.Background(), filed.EpicID); err != nil {
		t.Fatalf("releasing %s again: %v", filed.EpicID, err)
	}
	if !strings.Contains(second.String(), "none of the 2 stories of "+filed.EpicID+" is held") {
		t.Errorf("expected the second release to say there was nothing held, got:\n%s", second)
	}

	after, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories after the second release: %v", err)
	}
	if strings.Join(apptest.IDs(before), ",") != strings.Join(apptest.IDs(after), ",") {
		t.Fatalf("expected releasing twice to change nothing, %v became %v",
			apptest.IDs(before), apptest.IDs(after))
	}
}

func TestReleasingAnEpicNobodyFiledWritesNothing(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	out := &strings.Builder{}

	_, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), "f-nope")
	if err == nil {
		t.Fatal("expected releasing an epic nobody filed to be refused")
	}
	if !strings.Contains(err.Error(), "f-nope") {
		t.Errorf("expected the refusal to name the epic, got %q", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected nothing to be printed about an epic that is not there, got:\n%s", out)
	}
	for _, story := range filed.Stories {
		detail, err := tracker.ShowStory(context.Background(), story.ID)
		if err != nil {
			t.Fatalf("reading back %s: %v", story.ID, err)
		}
		if !detail.Held() {
			t.Errorf("expected %s to be held still, it is %s", story.ID, detail.Status)
		}
	}
}

// stuckTracker releases nothing: the way a tracker whose database is locked
// behaves once the reading has already succeeded.
type stuckTracker struct {
	*apptest.FakeTracker
}

func (s *stuckTracker) ReleaseStory(context.Context, string) error {
	return errors.New("the database is locked")
}

func TestAReleaseThatCannotWriteSaysWhichStory(t *testing.T) {
	tracker, filed := filedAndHeld(t)

	_, err := application.Release{Tracker: &stuckTracker{FakeTracker: tracker}}.
		Run(context.Background(), filed.EpicID)
	if err == nil {
		t.Fatal("expected a release the tracker refuses to fail")
	}
	for _, want := range []string{filed.Stories[0].ID, filed.EpicID, "the database is locked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the failure to say %q, got %q", want, err)
		}
	}
}

func TestReleasingNeedsAWorkTrackerAndAnEpic(t *testing.T) {
	if _, err := (application.Release{}).Run(context.Background(), "f-1"); err == nil {
		t.Error("expected releasing with no work tracker to be refused")
	}
	if _, err := (application.Release{Tracker: apptest.NewFakeTracker()}).Run(context.Background(), "  "); err == nil {
		t.Error("expected releasing with no epic to be refused")
	}
}

func TestReleaseStoryIdReleasesThatStoryAlone(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module, gateway := filed.Stories[0].ID, filed.Stories[1].ID
	out := &strings.Builder{}

	found, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), module)
	if err != nil {
		t.Fatalf("releasing the story %s: %v", module, err)
	}
	if !found.Released {
		t.Error("expected the release to report that it released the story")
	}
	for _, want := range []string{module + " · Go module", "millwright/main", "released 1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the release to say %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), gateway) {
		t.Errorf("expected the sibling %s not to be mentioned, got:\n%s", gateway, out)
	}

	released, err := tracker.ShowStory(context.Background(), module)
	if err != nil {
		t.Fatalf("reading back %s: %v", module, err)
	}
	if released.Held() {
		t.Errorf("expected %s to be released, it is %s", module, released.Status)
	}
	sibling, err := tracker.ShowStory(context.Background(), gateway)
	if err != nil {
		t.Fatalf("reading back %s: %v", gateway, err)
	}
	if !sibling.Held() {
		t.Errorf("expected %s to be held still, it is %s", gateway, sibling.Status)
	}
}

func TestReleaseStoryIdThatIsNotHeldLeavesItAsItIs(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	if err := tracker.ReleaseStory(context.Background(), module); err != nil {
		t.Fatalf("releasing %s first: %v", module, err)
	}
	out := &strings.Builder{}

	if _, err := (application.Release{Tracker: tracker, Out: out}).Run(context.Background(), module); err != nil {
		t.Fatalf("releasing the open story %s: %v", module, err)
	}
	if want := module + " is open, not held: nothing released"; !strings.Contains(out.String(), want) {
		t.Errorf("expected the release to say %q, got:\n%s", want, out)
	}
	if strings.Contains(out.String(), "released 1") {
		t.Errorf("expected no story to be released, got:\n%s", out)
	}
	detail, err := tracker.ShowStory(context.Background(), module)
	if err != nil {
		t.Fatalf("reading back %s: %v", module, err)
	}
	if detail.Status != apptest.StatusOpen {
		t.Errorf("expected %s to stay open, it is %s", module, detail.Status)
	}
}

func TestReleaseStoryIdWithNoRigIsRefusedAndWritesNothing(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("e-bare", domain.Path{})
	tracker.AddStory("e-bare", domain.Story{ID: "s-bare", Title: "No path"})
	if err := tracker.SetStatus("s-bare", apptest.StatusDeferred); err != nil {
		t.Fatalf("holding the story: %v", err)
	}
	out := &strings.Builder{}

	_, err := application.Release{Tracker: tracker, Out: out}.Run(context.Background(), "s-bare")
	if err == nil {
		t.Fatal("expected a story with no rig to be refused")
	}
	for _, want := range []string{"s-bare", "no path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got %q", want, err)
		}
	}
	detail, err := tracker.ShowStory(context.Background(), "s-bare")
	if err != nil {
		t.Fatalf("reading back s-bare: %v", err)
	}
	if !detail.Held() {
		t.Errorf("expected s-bare to be held still, it is %s", detail.Status)
	}
}

// expectsFiles says every rig expects a files key.
type expectsFiles struct{}

func (expectsFiles) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{FilesExpected: true}, nil
}

func TestReleaseStoryIdWarnsOfAMissingFilesKeyAndReleasesAllTheSame(t *testing.T) {
	tracker, filed := filedAndHeld(t)
	module := filed.Stories[0].ID
	out := &strings.Builder{}

	if _, err := (application.Release{Tracker: tracker, Rules: expectsFiles{}, Out: out}).Run(context.Background(), module); err != nil {
		t.Fatalf("releasing the story %s: %v", module, err)
	}
	if !strings.Contains(out.String(), module) || !strings.Contains(out.String(), "files") {
		t.Errorf("expected a warning that %s has no files key, got:\n%s", module, out)
	}
	if !strings.Contains(out.String(), "released 1") {
		t.Errorf("expected the story to be released all the same, got:\n%s", out)
	}
}
