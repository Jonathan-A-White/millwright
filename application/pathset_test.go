package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aVaultWithTwoFormulas is a fake vault that has installed chore and
// tdd-feature, and an epic with two open stories: a sibling pathed with
// tdd-feature and a story with no formula of its own.
func aVaultWithTwoFormulas() *apptest.FakeTracker {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("chore")
	tracker.AddFormula("tdd-feature")
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "laptop"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "sibling",
		Overrides: domain.Path{Formula: "tdd-feature"}})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "target"})
	return tracker
}

func TestPathSetRefusesAFormulaThatIsNotInstalledAndWritesNothing(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	writes := tracker.Writes()

	_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", []string{"formula=nope", "model=sonnet"})
	if err == nil {
		t.Fatal("expected formula=nope to be refused")
	}
	for _, said := range []string{"nope", "not installed", "chore", "tdd-feature", "nothing was written"} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
	if tracker.Writes() != writes {
		t.Errorf("expected nothing written, the tracker took %d writes", tracker.Writes()-writes)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Story.Overrides != (domain.Path{}) {
		t.Errorf("expected the story's metadata unchanged, got %+v", detail.Story.Overrides)
	}
}

func TestPathSetRefusesASiblingsFormulaThatIsNotInstalled(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.3", Title: "bug sibling",
		Overrides: domain.Path{Rig: "millwright", Formula: "bug"}})

	_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "mw-gq6.3", nil)
	if err == nil || !strings.Contains(err.Error(), "bug") || !strings.Contains(err.Error(), "tdd-feature") {
		t.Fatalf("expected the copied formula bug to be refused naming the installed ones, got %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Story.Overrides != (domain.Path{}) {
		t.Errorf("expected the story's metadata unchanged, got %+v", detail.Story.Overrides)
	}
}

func TestPathSetStillWritesAnInstalledFormula(t *testing.T) {
	tracker := aVaultWithTwoFormulas()

	path, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"formula=chore"})
	if err != nil {
		t.Fatalf("setting an installed formula: %v", err)
	}
	if path.Formula != "chore" {
		t.Errorf("expected formula chore, got %q", path.Formula)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Story.Overrides.Formula != "chore" {
		t.Errorf("expected formula chore written, got %+v", detail.Story.Overrides)
	}
}
