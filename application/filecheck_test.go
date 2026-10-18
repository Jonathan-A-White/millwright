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

// mw file --check reads a plan as mw file does, says every refusal across every
// story at once, and files nothing (mw-gq6.609).
func TestCheckSaysEveryRefusalOfEveryStoryAndFilesNothing(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Stories[0].Acceptance = "make test passes"
	plan.Stories[1].Overrides.Rig = "lampas"
	plan.Stories[1].Overrides.Branch = ""
	plan.Epic.Defaults.Branch = ""
	out := &strings.Builder{}

	err := application.File{Tracker: tracker, Out: out}.Check(context.Background(), plan)

	var refused *domain.PlanRefused
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal, got %v", err)
	}
	text := err.Error()
	if !strings.Contains(text, "story module") || !strings.Contains(text, "Expect") {
		t.Errorf("expected the refusal to name the missing Expect line of module, got:\n%s", text)
	}
	if !strings.Contains(text, "story gateway has no path") {
		t.Errorf("expected the refusal to name the path of gateway, got:\n%s", text)
	}
	if tracker.Writes() != 0 || len(tracker.Epics()) != 0 || len(tracker.Stories()) != 0 {
		t.Errorf("expected nothing filed, got %d writes, epics %v, stories %v", tracker.Writes(), tracker.Epics(), tracker.Stories())
	}
}

func TestCheckOfACleanPlanSaysHowManyStoriesWouldBeFiled(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	out := &strings.Builder{}

	if err := (application.File{Tracker: tracker, Out: out}).Check(context.Background(), twoStoryPlan()); err != nil {
		t.Fatalf("checking a clean plan: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "ok: 2 stories would be filed" {
		t.Errorf("got %q", got)
	}
	if tracker.Writes() != 0 || len(tracker.Epics()) != 0 || len(tracker.Stories()) != 0 {
		t.Errorf("expected nothing filed, got %d writes", tracker.Writes())
	}
}

func TestCheckCollectsTheFormulaAndTesterRefusalsToo(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	plan := twoStoryPlan() // tdd-feature is not installed on this tracker
	plan.Stories[0].Overrides.Formula = "tester"
	plan.Stories[1].Overrides.Formula = "tdd-feature"
	tracker.AddFormula("tester")

	err := application.File{Tracker: tracker}.Check(context.Background(), plan)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"story module", "Landed story", "story gateway"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, err)
		}
	}
}
