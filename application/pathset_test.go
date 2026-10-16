package application_test

import (
	"context"
	"slices"
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

// aDemoEpic is an epic of lampas with a first story, a held demo that waits on
// it, and a story S filed after the demo.
func aDemoEpic(t *testing.T) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "Build it"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.2", Title: "Show it"})
	if err := tracker.SetLabels("mw-e.2", application.LabelDemo); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStatus("mw-e.2", apptest.StatusDeferred); err != nil {
		t.Fatal(err)
	}
	tracker.Needs("mw-e.2", "mw-e.1")
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.3", Title: "A bug found later"})
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{LastStoryLabels: []string{"demo"}})
	return tracker, rules
}

func TestPathSetMakesTheEpicsDemoWaitOnTheStoryAndSaysSo(t *testing.T) {
	tracker, rules := aDemoEpic(t)
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out}).Run(context.Background(), "mw-e.3", "", []string{"rig=lampas"}); err != nil {
		t.Fatal(err)
	}
	if needs := needsOf(t, tracker, "mw-e.2"); !slices.Contains(needs, "mw-e.3") || !slices.Contains(needs, "mw-e.1") {
		t.Errorf("expected the demo to wait on mw-e.1 and mw-e.3, got %q", needs)
	}
	if said := out.String(); !strings.Contains(said, "mw-e.2 now waits on mw-e.3") {
		t.Errorf("expected a line naming the demo and the story, got %q", said)
	}

	// Setting the path again adds nothing and says nothing more.
	out.Reset()
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out}).Run(context.Background(), "mw-e.3", "", []string{"model=sonnet"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "waits on") {
		t.Errorf("expected no second line, got %q", out.String())
	}
}

func TestPathSetGivesNoEdgeToAStoryThatWaitsOnTheDemo(t *testing.T) {
	tracker, rules := aDemoEpic(t)
	tracker.Needs("mw-e.3", "mw-e.2")
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out}).Run(context.Background(), "mw-e.3", "", []string{"rig=lampas"}); err != nil {
		t.Fatal(err)
	}
	if needs := needsOf(t, tracker, "mw-e.2"); slices.Contains(needs, "mw-e.3") {
		t.Errorf("expected no edge that closes a cycle, got %q", needs)
	}
	if said := out.String(); !strings.Contains(said, "mw-e.3 waits on the epic's last story mw-e.2") || !strings.Contains(said, "cycle") {
		t.Errorf("expected the line to say why, got %q", said)
	}

	// The demo itself gets no edge on itself.
	out.Reset()
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out}).Run(context.Background(), "mw-e.2", "", []string{"rig=lampas"}); err != nil {
		t.Fatal(err)
	}
	if needs := needsOf(t, tracker, "mw-e.2"); slices.Contains(needs, "mw-e.2") {
		t.Errorf("the demo waits on itself: %q", needs)
	}
}

func TestPathSetAddsNothingWithoutExactlyOneDemoOrWithoutTheRigAskingForOne(t *testing.T) {
	cases := map[string]func(*apptest.FakeTracker, *apptest.FakeEpicRules){
		"no demo": func(tr *apptest.FakeTracker, _ *apptest.FakeEpicRules) {
			if err := tr.SetLabels("mw-e.2"); err != nil {
				t.Fatal(err)
			}
		},
		"two demos": func(tr *apptest.FakeTracker, _ *apptest.FakeEpicRules) {
			tr.AddStory("mw-e", domain.Story{ID: "mw-e.4", Title: "Show it again"})
			if err := tr.SetLabels("mw-e.4", application.LabelDemo); err != nil {
				t.Fatal(err)
			}
		},
		"a rig that asks for none": func(_ *apptest.FakeTracker, rules *apptest.FakeEpicRules) {
			rules.Require("lampas", domain.EpicRequirements{})
		},
	}
	for name, change := range cases {
		tracker, rules := aDemoEpic(t)
		change(tracker, rules)
		var out strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out}).Run(context.Background(), "mw-e.3", "", []string{"rig=lampas"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ids := []string{"mw-e.1", "mw-e.2"}
		if name == "two demos" {
			ids = append(ids, "mw-e.4")
		}
		for _, id := range ids {
			if needs := needsOf(t, tracker, id); slices.Contains(needs, "mw-e.3") {
				t.Errorf("%s: %s was made to wait on mw-e.3: %q", name, id, needs)
			}
		}
		if !strings.HasPrefix(out.String(), "Set the path of mw-e.3:") || strings.Count(out.String(), "\n") != 1+len(domain.Fields) {
			t.Errorf("%s: expected the path echo alone, got %q", name, out.String())
		}
	}
}
