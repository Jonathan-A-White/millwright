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

// twoStoryPlan is the smallest plan worth filing: one story that waits on
// nothing and one that waits on it.
func twoStoryPlan() domain.Plan {
	return domain.Plan{
		Epic: domain.PlanEpic{
			Key:      "skeleton",
			Title:    "Walking skeleton",
			Priority: 1,
			Success:  "One story goes from the plan file to a commit.",
			Defaults: domain.Path{
				Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
				Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
			},
		},
		Stories: []domain.PlanStory{
			{Key: "module", Title: "Go module", Acceptance: "make test passes", Estimate: 60},
			{Key: "gateway", Title: "Beads gateway", Acceptance: "make test passes", Needs: []string{"module"}},
		},
	}
}

func TestAPlanNobodyApprovedStaysHeld(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	out := &strings.Builder{}

	filed, err := application.File{
		Tracker: tracker,
		Out:     out,
		Approve: func(context.Context, application.FiledPlan) (bool, error) { return false, nil },
	}.Run(context.Background(), twoStoryPlan())
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}
	if filed.Released {
		t.Error("expected a plan nobody approved not to be released")
	}

	ready, err := tracker.ReadyStories(context.Background(), filed.EpicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("expected nothing to be ready, got %v", apptest.IDs(ready))
	}
	// The person is told plainly that filing again is not how to release them.
	for _, want := range []string{"nothing here can be dispatched", "would file a second copy", "bd update " + filed.Stories[0].ID} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the report to say %q, got:\n%s", want, out.String())
		}
	}
}

func TestAnApprovedPlanSaysWhatIsReadyNow(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	out := &strings.Builder{}

	filed, err := application.File{
		Tracker: tracker,
		Out:     out,
		Approve: func(context.Context, application.FiledPlan) (bool, error) { return true, nil },
	}.Run(context.Background(), twoStoryPlan())
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}
	if !filed.Released {
		t.Fatal("expected an approved plan to be released")
	}
	if got := filed.Unblocked(); len(got) != 1 || got[0] != filed.Stories[0].ID {
		t.Fatalf("expected only the story that waits on nothing to be ready, got %v", got)
	}
	if !strings.Contains(out.String(), "Ready now: "+filed.Stories[0].ID) {
		t.Errorf("expected the report to say what is ready now, got:\n%s", out.String())
	}
	// The estimate and what each story waits on are in the tree.
	if !strings.Contains(out.String(), "60m") || !strings.Contains(out.String(), "waits on "+filed.Stories[0].ID) {
		t.Errorf("expected the tree to carry the estimate and what the second story waits on, got:\n%s", out.String())
	}
}

func TestAPlanIsNotFiledAtAllWhenItCannotBeFiled(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	plan := twoStoryPlan()
	plan.Epic.Defaults.Branch = ""

	if _, err := (application.File{Tracker: tracker}).Run(context.Background(), plan); err == nil {
		t.Fatal("expected a plan whose stories have no branch to be refused")
	}
	if epics, stories := tracker.Epics(), tracker.Stories(); len(epics) != 0 || len(stories) != 0 {
		t.Fatalf("expected nothing to be written, got epics %v and stories %v", epics, stories)
	}
}

// halfATracker files the epic and the first story, then refuses — the way a
// tracker that dies halfway through does.
type halfATracker struct {
	*apptest.FakeTracker
	filed int
}

func (h *halfATracker) CreateStory(ctx context.Context, story application.NewStory) (string, error) {
	if h.filed > 0 {
		return "", errors.New("the database is locked")
	}
	h.filed++
	return h.FakeTracker.CreateStory(ctx, story)
}

func TestFilingThatStopsHalfwaySaysWhatIsAlreadyFiled(t *testing.T) {
	tracker := &halfATracker{FakeTracker: apptest.NewFakeTracker()}

	_, err := (application.File{Tracker: tracker}).Run(context.Background(), twoStoryPlan())
	if err == nil {
		t.Fatal("expected filing to fail when the tracker refuses a story")
	}
	for _, want := range []string{"gateway", "the epic and 1 of its stories are filed and held", "the database is locked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the failure to say %q, got %q", want, err)
		}
	}
	// What was filed is held, so a half-filed plan is still not dispatchable.
	ready, err := tracker.ReadyStories(context.Background(), tracker.Epics()[0], "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("expected a half-filed plan to leave nothing ready, got %v", apptest.IDs(ready))
	}
}

func TestFilingNeedsAWorkTracker(t *testing.T) {
	if _, err := (application.File{}).Run(context.Background(), twoStoryPlan()); err == nil {
		t.Fatal("expected filing with no work tracker to be refused")
	}
}
