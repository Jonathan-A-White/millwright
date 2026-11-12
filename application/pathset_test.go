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

// aReleasingRig is a fake vault with a held story S pathed from a sibling, a
// closed-over story B for it to wait on, and a rig file asking to release on
// path set.
func aReleasingRig(t *testing.T) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker := aVaultWithTwoFormulas()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.3", Title: "needed first"})
	if err := tracker.SetStatus("mw-gq6.2", apptest.StatusDeferred); err != nil {
		t.Fatal(err)
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("millwright", domain.EpicRequirements{ReleaseOnPath: true})
	return tracker, rules
}

func TestPathSetAfterReleasesTheHeldStoryLastOnARigThatReleasesOnPathSet(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	var out strings.Builder

	_, err := application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	if detail.Held() || !slices.Contains(detail.Needs, "mw-gq6.3") {
		t.Errorf("expected the story open and waiting on mw-gq6.3, got status %s, needs %v", detail.Status, detail.Needs)
	}
	comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2")
	if len(comments) != 1 || comments[0].Text != application.ReleasedOnPathSet {
		t.Errorf("expected the one comment %q, got %+v", application.ReleasedOnPathSet, comments)
	}
}

func TestPathSetAfterNoneReleasesAndNoAfterNeverDoes(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path with no --after: %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); !detail.Held() {
		t.Errorf("expected the story held with no --after, got %s", detail.Status)
	}
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, After: []string{}}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path with --after none: %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Held() {
		t.Errorf("expected the story released by --after none, got %s", detail.Status)
	}
}

func TestPathSetAfterOnARigWithoutTheKeyOnlyAddsTheDependency(t *testing.T) {
	tracker, _ := aReleasingRig(t)
	rules := apptest.NewFakeEpicRules()
	for _, withRules := range []application.EpicRules{nil, rules} {
		set := application.PathSet{Tracker: tracker, After: []string{"mw-gq6.3"}}
		if withRules != nil {
			set.Rules = withRules
		}
		if _, err := set.Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
			t.Fatalf("setting the path: %v", err)
		}
		detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
		if !detail.Held() || !slices.Contains(detail.Needs, "mw-gq6.3") {
			t.Errorf("expected the story held and waiting on mw-gq6.3, got %s, %v", detail.Status, detail.Needs)
		}
	}
}

func TestPathSetHoldWritesTheReasonAndKeepsTheStoryHeldEvenWithAfter(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	_, err := application.PathSet{Tracker: tracker, Rules: rules, After: []string{"mw-gq6.3"}, Hold: "waits for the demo"}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil)
	if err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	comments, _ := tracker.StoryComments(context.Background(), "mw-gq6.2")
	if !detail.Held() || len(comments) != 1 || comments[0].Text != "HELD: waits for the demo" {
		t.Errorf("expected the story held with HELD: waits for the demo, got %s, %+v", detail.Status, comments)
	}
}

func TestPathSetRefusesBadAfterAndHoldBeforeWritingAnything(t *testing.T) {
	for name, set := range map[string]application.PathSet{
		"a missing story":   {After: []string{"mw-gq6.99"}},
		"the story itself":  {After: []string{"mw-gq6.2"}},
		"an empty id":       {After: []string{" "}},
		"a blank hold":      {Hold: "  "},
		"a hold on an open": {Hold: "why", After: []string{"mw-gq6.3"}},
	} {
		tracker, rules := aReleasingRig(t)
		if name == "a hold on an open" {
			if err := tracker.SetStatus("mw-gq6.2", apptest.StatusOpen); err != nil {
				t.Fatal(err)
			}
		}
		set.Tracker, set.Rules = tracker, rules
		writes := tracker.Writes()
		if _, err := set.Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
		if tracker.Writes() != writes {
			t.Errorf("%s: expected nothing written, the tracker took %d writes", name, tracker.Writes()-writes)
		}
	}
}

func TestPathSetAfterStaysHeldWhenTheEpicHoldsItOnTheGovernorsWord(t *testing.T) {
	tracker, rules := aReleasingRig(t)
	tracker.AddEpicComment("mw-gq6", "HELD: mw-gq6.2 waits for the Governor's word on the colour")
	var out strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Out: &out, After: []string{"mw-gq6.3"}}).
		Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); !detail.Held() {
		t.Errorf("expected the story held on the Governor's word, got %s", detail.Status)
	}
	if !strings.Contains(out.String(), "held: mw-gq6.2: it is held on the Governor's word") {
		t.Errorf("expected a held: line naming the Governor's word, got:\n%s", out.String())
	}
}

// mw-6ww.163.1: a path naming a host the Governor retired is refused, naming
// the config key, and nothing is written.
func TestPathSetRefusesARetiredHostNamingTheKeyAndWritesNothing(t *testing.T) {
	for _, host := range []string{"desktop", "vps"} {
		tracker := aVaultWithTwoFormulas()
		writes := tracker.Writes()

		_, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop", "vps"}}.
			Run(context.Background(), "mw-gq6.2", "", []string{"host=" + host})
		if err == nil {
			t.Fatalf("expected host=%s to be refused", host)
		}
		for _, said := range []string{host, "retired_hosts", "nothing was written"} {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("host=%s: expected the refusal to say %q, got %q", host, said, err)
			}
		}
		if tracker.Writes() != writes {
			t.Errorf("host=%s: expected nothing written, got %d writes", host, tracker.Writes()-writes)
		}
	}
}

func TestPathSetStillTakesAHostThatIsNotRetired(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	_, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop"}}.
		Run(context.Background(), "mw-gq6.2", "", []string{"host=laptop"})
	if err != nil {
		t.Fatalf("host=laptop: %v", err)
	}
}

// A sibling's retired host, copied by --from, is refused too.
func TestPathSetRefusesARetiredHostCopiedFromASibling(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.3", Title: "on the desktop",
		Overrides: domain.Path{Rig: "millwright", Host: "desktop"}})
	_, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop"}}.
		Run(context.Background(), "mw-gq6.2", "mw-gq6.3", nil)
	if err == nil || !strings.Contains(err.Error(), "retired_hosts") {
		t.Fatalf("expected the copied host to be refused naming retired_hosts, got %v", err)
	}
}

func TestPathSetWithNeitherAfterNorHoldOnAReleasingRigSaysTheStoryStaysHeld(t *testing.T) {
	const line = `held: no --after given; pass --after none to release or --hold "<why>" to keep it held`
	for name, set := range map[string]application.PathSet{
		"neither":    {},
		"after":      {After: []string{"mw-gq6.3"}},
		"after none": {After: []string{}},
		"hold":       {Hold: "waits for the demo"},
	} {
		tracker, rules := aReleasingRig(t)
		var errOut strings.Builder
		set.Tracker, set.Rules, set.Err = tracker, rules, &errOut
		if _, err := set.Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
			t.Fatalf("%s: setting the path: %v", name, err)
		}
		got := errOut.String()
		if name == "neither" {
			if got != line+"\n" {
				t.Errorf("%s: expected stderr to be the one line %q, got %q", name, line, got)
			}
		} else if strings.Contains(got, "no --after given") {
			t.Errorf("%s: expected no held line, got %q", name, got)
		}
	}
}

func TestPathSetWithNeitherAfterNorHoldOnARigWithoutTheKeySaysNothing(t *testing.T) {
	tracker, _ := aReleasingRig(t)
	var errOut strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut.String(), "no --after given") {
		t.Errorf("a rig without release_on_path: expected no held line, got %q", errOut.String())
	}
}
