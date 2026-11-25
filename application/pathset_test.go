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

// mw-gq6.1165: a path naming a host the Governor retired is written as
// host=auto, and the output says so (it was refused, mw-6ww.163.1). The cases
// are in repath_test.go.

func TestPathSetStillTakesAHostThatIsNotRetired(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	_, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop"}}.
		Run(context.Background(), "mw-gq6.2", "", []string{"host=laptop"})
	if err != nil {
		t.Fatalf("host=laptop: %v", err)
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

// aFilesExpectedRig is the vault of aVaultWithTwoFormulas on a rig whose file
// says files_expected = true.
func aFilesExpectedRig(expects bool) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	tracker := aVaultWithTwoFormulas()
	rules := apptest.NewFakeEpicRules()
	rules.Require("millwright", domain.EpicRequirements{FilesExpected: expects})
	return tracker, rules
}

func TestPathSetFromOnARigThatExpectsFilesSaysTheFilesWereNotCopied(t *testing.T) {
	tracker, rules := aFilesExpectedRig(true)
	var errOut strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	const line = "files not copied from mw-gq6.1: pass files=\n"
	if errOut.String() != line {
		t.Errorf("expected stderr to be the one line %q, got %q", line, errOut.String())
	}
}

func TestPathSetFromSaysNothingOfFilesWhenFilesAreGiven(t *testing.T) {
	tracker, rules := aFilesExpectedRig(true)
	var errOut strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"files=a.go"}); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if strings.Contains(errOut.String(), "files not copied") {
		t.Errorf("expected no files line when files= is given, got %q", errOut.String())
	}
}

func TestPathSetFromSaysNothingOfFilesOnARigThatDoesNotExpectThem(t *testing.T) {
	tracker, rules := aFilesExpectedRig(false)
	var errOut strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if strings.Contains(errOut.String(), "files not copied") {
		t.Errorf("expected no files line on a rig that does not expect files, got %q", errOut.String())
	}
}

func TestPathSetWithoutFromSaysNothingOfFiles(t *testing.T) {
	tracker, rules := aFilesExpectedRig(true)
	var errOut strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "", []string{"model=sonnet"}); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if strings.Contains(errOut.String(), "files not copied") {
		t.Errorf("expected no files line without --from, got %q", errOut.String())
	}
}

func TestPathSetFromStillCopiesNoFiles(t *testing.T) {
	tracker, rules := aFilesExpectedRig(true)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.4", Title: "with files",
		Overrides: domain.Path{Rig: "millwright", Files: "a.go", Formula: "tdd-feature"}})

	if _, err := (application.PathSet{Tracker: tracker, Rules: rules}).Run(context.Background(), "mw-gq6.2", "mw-gq6.4", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	if detail.Story.Overrides.Files != "" || detail.Story.Overrides.Formula != "tdd-feature" {
		t.Errorf("expected the formula copied and the files not, got %+v", detail.Story.Overrides)
	}
}

// mw-gq6.1288: a story whose path says why its host must stay (host_why) keeps
// it: mw path set will not write another host over it, whatever the host is.
func TestPathSetRefusesAnotherHostOnAStoryWithAHostWhy(t *testing.T) {
	const why = "the Morpheus cache is made on the Laptop"
	for name, pairs := range map[string][]string{
		"auto":         {"host=auto"},
		"a cloud box":  {"host=cloud2"},
		"with a model": {"model=sonnet", "host=auto"},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := aVaultWithTwoFormulas()
			pinnedTo(tracker, "mw-gq6.3", "laptop", domain.HostWhyField, why)
			var out strings.Builder
			_, err := application.PathSet{Tracker: tracker, Out: &out}.Run(context.Background(), "mw-gq6.3", "", pairs)
			if err == nil {
				t.Fatalf("path set %v on a story with a host_why wrote it", pairs)
			}
			for _, want := range []string{why, "host_why", "nothing was written", "--unset-metadata host_why"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal lacks %q: %v", want, err)
				}
			}
			detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.3")
			if got := detail.Merged(); got.Host != "laptop" || got.Model == domain.ModelSonnet || got.HostWhy != why {
				t.Errorf("a refused set changed the path: %+v", got)
			}
		})
	}
}

func TestPathSetRefusesACopiedHostOnAStoryWithAHostWhy(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.3", "laptop", domain.HostWhyField, "the Morpheus cache is on the Laptop")
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.4", Title: "sibling on a box",
		Overrides: domain.Path{Rig: "lampas", Host: "cloud2"}})
	if _, err := (application.PathSet{Tracker: tracker}).Run(context.Background(), "mw-gq6.3", "mw-gq6.4", nil); err == nil {
		t.Fatal("path set --from a sibling on another host moved a story with a host_why")
	}
	if got := storyHost(t, tracker, "mw-gq6.3"); got != "laptop" {
		t.Errorf("host = %q, want laptop kept", got)
	}
}

func TestPathSetStillTakesTheSameHostOrANewWhyOnAStoryWithAHostWhy(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.3", "laptop", domain.HostWhyField, "the Morpheus cache is on the Laptop")
	ctx := context.Background()
	if _, err := (application.PathSet{Tracker: tracker}).Run(ctx, "mw-gq6.3", "", []string{"host=laptop", "model=sonnet"}); err != nil {
		t.Fatalf("the same host: %v", err)
	}
	// On purpose: the new host with the reason it must stay there.
	if _, err := (application.PathSet{Tracker: tracker}).Run(ctx, "mw-gq6.3", "", []string{"host=desktop", "host_why=the GPU is there"}); err != nil {
		t.Fatalf("a new host with a new why: %v", err)
	}
	if got := storyHost(t, tracker, "mw-gq6.3"); got != "desktop" {
		t.Errorf("host = %q, want desktop", got)
	}
}

// A story with no host_why is moved as ever.
func TestPathSetStillMovesAStoryWithNoHostWhy(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	if _, err := (application.PathSet{Tracker: tracker}).Run(context.Background(), "mw-gq6.2", "", []string{"host=auto"}); err != nil {
		t.Fatalf("host=auto on a story with no host_why: %v", err)
	}
}

// A host the Governor retired is written as host=auto instead (mw-gq6.1165), and
// that is another host: a pinned story naming its own retired host keeps it
// refused rather than moved.
func TestPathSetDoesNotMoveAPinnedStoryToAutoByWayOfItsRetiredHost(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.3", "desktop", domain.HostWhyField, "the GPU is on the desktop")
	_, err := application.PathSet{Tracker: tracker, Retired: []string{"desktop"}}.
		Run(context.Background(), "mw-gq6.3", "", []string{"host=desktop"})
	if err == nil {
		t.Fatal("a retired host written over a pinned story was turned into host=auto")
	}
	if got := storyHost(t, tracker, "mw-gq6.3"); got != "desktop" {
		t.Errorf("host = %q, want desktop kept", got)
	}
}

// mw-gq6.1288 AC 2: an epic whose metadata carries host and host_why gives both
// to the stories filed under it, and mw path set refuses another host for them.
func TestAStoryOfAnEpicWithAHostWhyKeepsItsHost(t *testing.T) {
	const why = "lampas docs/lxx-derivation.md:552: model stories are pathed host=laptop"
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-u2w5klk", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", HostWhy: why})
	tracker.AddStory("mw-u2w5klk", domain.Story{ID: "mw-u2w5klk.238", Title: "a model story"})

	detail, _ := tracker.ShowStory(context.Background(), "mw-u2w5klk.238")
	if got := detail.Merged(); got.Host != "laptop" || got.HostWhy != why {
		t.Fatalf("the story has host=%q host_why=%q, want the epic's", got.Host, got.HostWhy)
	}
	_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-u2w5klk.238", "", []string{"host=auto"})
	if err == nil || !strings.Contains(err.Error(), why) {
		t.Fatalf("host=auto on a story of an epic with a host_why: want a refusal naming the why, got %v", err)
	}
	if got := storyHost(t, tracker, "mw-u2w5klk.238"); got != "laptop" {
		t.Errorf("host = %q, want laptop kept", got)
	}
}

// aStoryWithAcceptance is the vault of aFilesExpectedRig(false) whose target
// story, mw-gq6.2, carries the given acceptance criteria and whose rig names a
// site_url.
func aStoryWithAcceptance(t *testing.T, acceptance string) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker, rules := aFilesExpectedRig(false)
	rules.Require("millwright", domain.EpicRequirements{SiteURL: "https://mill.example.net/"})
	if err := tracker.SetAcceptance("mw-gq6.2", acceptance); err != nil {
		t.Fatal(err)
	}
	return tracker, rules
}

func TestPathSetWarnsOfACriterionThatNeedsTheLiveSiteOrALandedFix(t *testing.T) {
	const moved = "move it out of the acceptance and into the Mayor's after-landing check"
	for name, c := range map[string]struct {
		acceptance string
		line       string
	}{
		"a served rig's live page": {"1. curl https://postern.allmymind.org/ shows the new banner", "criterion 1"},
		"the rig's site_url":       {"1. it builds.\n2. https://mill.example.net/status answers 200", "criterion 2"},
		"after the fix lands":      {"1. the page shows the banner after the fix lands", "criterion 1"},
		"after landing":            {"1. the page shows the banner After Landing", "criterion 1"},
		"once it is live":          {"1. the banner shows once it is live", "criterion 1"},
	} {
		tracker, rules := aStoryWithAcceptance(t, c.acceptance)
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
			t.Fatalf("%s: nothing is refused for it, got %v", name, err)
		}
		got := errOut.String()
		if strings.Count(got, "WARN") != 1 || !strings.Contains(got, "mw-gq6.2") || !strings.Contains(got, c.line) ||
			!strings.Contains(got, moved) || !strings.Contains(got, "Verify strings") {
			t.Errorf("%s: expected one warning naming the story and %s, and saying to %s; got %q", name, c.line, moved, got)
		}
	}
}

func TestPathSetWarnsOnceOfAnAcceptanceWithTwoSuchCriteria(t *testing.T) {
	tracker, rules := aStoryWithAcceptance(t, "1. curl https://postern.allmymind.org/ shows it\n2. it is live once it is live")
	var errOut strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatal(err)
	}
	if got := errOut.String(); strings.Count(got, "WARN") != 1 || !strings.Contains(got, "criterion 1, 2") {
		t.Errorf("expected one warning naming both criteria, got %q", got)
	}
}

func TestPathSetSaysNothingOfACriterionAnyBuilderCanRun(t *testing.T) {
	for name, acceptance := range map[string]string{
		"neither":              "1. go test ./application passes\n2. mw path show prints the key",
		"a URL in a test":      "1. A test in a_test.go fakes a page at https://postern.allmymind.org/ and sees the banner",
		"a URL in a fixture":   "1. the fixture holds https://mill.example.net/ and the parser reads it",
		"a Verify line":        "1. it builds.\nVerify strings: the banner",
		"a landing in a test":  "1. a test of the order after landing passes",
		"a URL of no live rig": "1. curl http://localhost:8080/ answers",
	} {
		tracker, rules := aStoryWithAcceptance(t, acceptance)
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if errOut.Len() != 0 {
			t.Errorf("%s: expected no warning, got %q", name, errOut.String())
		}
	}
}

// aMediaRig is nac-service-media's story (mw-pniowru) on a host whose checkout
// of the rig has an origin with only master.
func aMediaRig(t *testing.T, readable bool) (*apptest.FakeTracker, map[string]string, *apptest.FakeRigFiles) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	tracker.AddEpic("mw-e", domain.Path{Rig: "nac-service-media", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "laptop"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "sibling", Overrides: domain.Path{Rig: "nac-service-media", Branch: "main"}})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.2", Title: "target"})
	files := apptest.NewFakeRigFiles()
	if readable {
		files.AddOrigin("/rigs/nac-service-media", "master", "master")
	}
	return tracker, map[string]string{"nac-service-media": "/rigs/nac-service-media"}, files
}

func TestPathSetRefusesABranchTheRigsOriginDoesNotHave(t *testing.T) {
	tracker, rigs, files := aMediaRig(t, true)
	writes := tracker.Writes()

	_, err := application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: files}.
		Run(context.Background(), "mw-e.2", "", []string{"rig=nac-service-media", "branch=main"})
	if err == nil {
		t.Fatal("expected branch=main to be refused on a rig whose origin has only master")
	}
	for _, said := range []string{"branch=main", "branch=master", "nothing was written"} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
	if tracker.Writes() != writes {
		t.Errorf("expected nothing written, the tracker took %d writes", tracker.Writes()-writes)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-e.2"); detail.Story.Overrides.Branch != "" {
		t.Errorf("expected no branch written, got %q", detail.Story.Overrides.Branch)
	}
}

func TestPathSetWritesABranchTheRigsOriginHas(t *testing.T) {
	tracker, rigs, files := aMediaRig(t, true)

	if _, err := (application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: files}).
		Run(context.Background(), "mw-e.2", "", []string{"rig=nac-service-media", "branch=master"}); err != nil {
		t.Fatalf("setting branch=master: %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-e.2"); detail.Story.Overrides.Branch != "master" {
		t.Errorf("expected branch master written, got %q", detail.Story.Overrides.Branch)
	}
}

func TestPathSetRefusesABranchCopiedFromASiblingThatTheOriginDoesNotHave(t *testing.T) {
	tracker, rigs, files := aMediaRig(t, true)

	_, err := application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: files}.
		Run(context.Background(), "mw-e.2", "mw-e.1", nil)
	if err == nil || !strings.Contains(err.Error(), "branch=master") {
		t.Fatalf("expected the copied branch=main to be refused naming branch=master, got %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-e.2"); detail.Story.Overrides.Branch != "" {
		t.Errorf("expected no branch written, got %q", detail.Story.Overrides.Branch)
	}
}

func TestPathSetWarnsAndWritesABranchWhenTheRigCheckoutCannotBeRead(t *testing.T) {
	tracker, rigs, files := aMediaRig(t, false)
	var errs strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: files, Err: &errs}).
		Run(context.Background(), "mw-e.2", "", []string{"rig=nac-service-media", "branch=main"}); err != nil {
		t.Fatalf("an unreadable checkout must not refuse: %v", err)
	}
	if !strings.Contains(errs.String(), "WARN:") || !strings.Contains(errs.String(), "branch=main") {
		t.Errorf("expected a WARN line naming branch=main, got %q", errs.String())
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-e.2"); detail.Story.Overrides.Branch != "main" {
		t.Errorf("expected branch main written, got %q", detail.Story.Overrides.Branch)
	}
}

func TestPathSetWarnsWhenTheRigIsNotCheckedOutHereAndAnExplicitBranchIsWritten(t *testing.T) {
	tracker, _, files := aMediaRig(t, true)
	var errs strings.Builder

	if _, err := (application.PathSet{Tracker: tracker, RigFiles: files, Err: &errs}).
		Run(context.Background(), "mw-e.2", "", []string{"rig=nac-service-media", "branch=main"}); err != nil {
		t.Fatalf("a rig not here must not refuse: %v", err)
	}
	if !strings.Contains(errs.String(), "WARN:") {
		t.Errorf("expected a WARN line, got %q", errs.String())
	}
}

func TestFileRefusesAStoryWhoseBranchTheRigsOriginDoesNotHave(t *testing.T) {
	_, rigs, files := aMediaRig(t, true)
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Epic.Defaults.Rig = "nac-service-media"
	plan.Epic.Defaults.Branch = "main"
	plan.Stories = []domain.PlanStory{{Key: "talk", Title: "Talk sends twice", Acceptance: "make test passes\nExpect: none, because a test plan"}}

	_, err := application.File{Tracker: tracker, Rigs: rigs, RigFiles: files}.Run(context.Background(), plan)
	if err == nil {
		t.Fatal("expected the plan to be refused")
	}
	for _, said := range []string{"talk", "branch=main", "branch=master"} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
	if epics := tracker.Epics(); len(epics) != 0 {
		t.Errorf("expected nothing filed, got %v", epics)
	}
}

func TestPathSetPinsAStoryNeedingServedBeadsToTheHome(t *testing.T) {
	for name, acceptance := range map[string]string{
		"the trace":     "1. it builds.\n2. go run ./contrib/dolt-beads/trace shows the read under 1 s",
		"BEADS_DOLT":    "1. BEADS_DOLT_PORT=3307 go test ./infrastructure/beads passes",
		"the port":      "1. a read against 127.0.0.1:3307 answers",
		"in a test too": "1. a test in a_test.go run against 127.0.0.1:3307 passes",
	} {
		tracker, rules := aStoryWithAcceptance(t, acceptance)
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"host=auto"}); err != nil {
			t.Fatalf("%s: nothing is refused for it, got %v", name, err)
		}
		got := errOut.String()
		if strings.Count(got, "WARN") != 1 || !strings.Contains(got, "mw-gq6.2") || !strings.Contains(got, "host=auto") ||
			!strings.Contains(got, "served beads") || !strings.Contains(got, "criterion ") {
			t.Errorf("%s: expected one warning naming the story, the criterion and host=auto; got %q", name, got)
		}
	}
}

func TestPathSetSaysNothingOfServedBeadsToAStoryPinnedToAHomeHost(t *testing.T) {
	tracker, rules := aStoryWithAcceptance(t, "1. go run ./contrib/dolt-beads/trace shows the read under 1 s")
	var errOut strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"host=laptop"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut.String(), "served beads") {
		t.Errorf("a story pinned to the laptop has its beads; got %q", errOut.String())
	}
}

// mw-gq6.1714: path set --from copies a host pin only with its host_why; a pin
// with no why is written as host=auto, and the output says so. The table is the
// rule of every other key --from copies or drops.
func TestPathSetFromCopiesAHostPinWithItsHostWhy(t *testing.T) {
	const why = "the Morpheus cache is made on the Laptop"
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.4", "laptop", domain.HostWhyField, why)
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6.2", "mw-gq6.4", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	if got := detail.Merged(); got.Host != "laptop" || got.HostWhy != why {
		t.Errorf("want host=laptop with its host_why, got host=%q host_why=%q", got.Host, got.HostWhy)
	}
	if strings.Contains(out.String(), "host_why") && strings.Contains(out.String(), "not copied") {
		t.Errorf("a pin copied with its why must not say it was not copied: %q", out.String())
	}
}

func TestPathSetFromWritesHostAutoForAPinWithNoHostWhyAndSaysSo(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.4", "laptop")
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6.2", "mw-gq6.4", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	if got := detail.Merged(); got.Host != domain.HostAuto || got.HostWhy != "" {
		t.Errorf("want host=auto and no host_why, got host=%q host_why=%q", got.Host, got.HostWhy)
	}
	for _, want := range []string{"host=laptop", "not copied", "host_why", "host=auto"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output lacks %q: %q", want, out.String())
		}
	}
}

// A host named on the command line is the command's own: the sibling's missing
// why does not turn it into auto.
func TestPathSetFromKeepsAHostNamedOnTheCommandOverASiblingPinWithNoWhy(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	pinnedTo(tracker, "mw-gq6.4", "laptop")
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-gq6.2", "mw-gq6.4", []string{"host=desktop"}); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	if got := storyHost(t, tracker, "mw-gq6.2"); got != "desktop" {
		t.Errorf("want host=desktop, got %q", got)
	}
	if strings.Contains(out.String(), "not copied") {
		t.Errorf("a host named on the command needs no such line: %q", out.String())
	}
}

// Every key --from copies or drops, and its rule.
func TestPathSetFromRuleOfEveryKey(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.4", Title: "sibling", Overrides: domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet,
		Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "laptop", HostWhy: "the cache is there",
		Size: domain.Size("S"), Bump: domain.BumpMinor, CallersOK: "the callers are mine", MaxTurns: "60", Files: "a.go",
	}})
	if _, err := (application.PathSet{Tracker: tracker}).Run(context.Background(), "mw-gq6.2", "mw-gq6.4", nil); err != nil {
		t.Fatalf("setting the path: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	got := detail.Merged()
	for key, rule := range map[string]struct{ got, want string }{
		"rig: copied":                         {got.Rig, "millwright"},
		"branch: copied":                      {got.Branch, "main"},
		"harness: copied":                     {string(got.Harness), string(domain.HarnessClaude)},
		"model: copied":                       {string(got.Model), string(domain.ModelSonnet)},
		"effort: copied":                      {string(got.Effort), string(domain.EffortHigh)},
		"formula: copied":                     {got.Formula, "tdd-feature"},
		"host: copied, with its host_why":     {got.Host, "laptop"},
		"host_why: copied, only with a host":  {got.HostWhy, "the cache is there"},
		"size: dropped, the story's own":      {string(got.Size), ""},
		"bump: dropped, the story's own":      {got.Bump, ""},
		"callers_ok: dropped, for the files":  {got.CallersOK, ""},
		"max_turns: copied, as it always was": {got.MaxTurns, "60"},
		"files: dropped, the story's own":     {got.Files, ""},
	} {
		if rule.got != rule.want {
			t.Errorf("%s: got %q, want %q", key, rule.got, rule.want)
		}
	}
}
