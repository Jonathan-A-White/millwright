package application_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// testerFileEpic is a fixture landing: the landed story mw-e.1 (closed) and its
// Tester story mw-e.2 under the epic mw-e of lampas, an open demo mw-e.3, a
// running story mw-e.4 and an open one mw-e.5 that name src/Reader.tsx, one that
// names another file, and an open bead that already holds one of the findings.
func testerFileEpic(t *testing.T) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "Reader remembers its place"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.2", Title: "Test: Reader remembers its place",
		Overrides: domain.Path{Formula: "tester"}})
	if err := tracker.SetDescription("mw-e.2", "Landed story: mw-e.1 (landed on main of lampas at abc1234)\n\nDrive it."); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetLabels("mw-e.2", application.LabelTester); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.3", Title: "Show it"})
	if err := tracker.SetLabels("mw-e.3", application.LabelDemo); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.4", Title: "Reader paging", Overrides: domain.Path{Files: "src/Reader.tsx"}})
	if err := tracker.SetStatus("mw-e.4", apptest.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.5", Title: "Reader themes", Overrides: domain.Path{Files: "src/Reader.tsx,src/theme.ts"}})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.6", Title: "Settings page", Overrides: domain.Path{Files: "src/Settings.tsx"}})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.7", Title: "[bug] lampas: The mic stays red after Back"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.8", Title: "Reader old fix", Overrides: domain.Path{Files: "src/Reader.tsx"}})
	if err := tracker.SetStatus("mw-e.8", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.9", Title: "Reader held idea", Overrides: domain.Path{Files: "src/Reader.tsx"}})
	if err := tracker.SetStatus("mw-e.9", apptest.StatusDeferred); err != nil {
		t.Fatal(err)
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{LastStoryLabels: []string{"demo"}})
	return tracker, rules
}

func testerFinding() application.TesterFinding {
	return application.TesterFinding{
		Kind:       "bug",
		Title:      "The page number is lost after rotate",
		Finding:    "Rotate to 844x390 and back: the Reader is on page 1 again.\nSteps: open page 7, rotate, rotate back.",
		Shot:       "/tmp/tester-mw-e.2/06-rotate.png",
		Fix:        "keep the page in state, not in the viewport effect",
		HowToCheck: "1. Open page 7.\n2. Rotate the phone and back: page 7 is still shown.",
		Acceptance: "npx vitest run src/Reader.test.tsx passes with the new rotate case\nSweep: every viewport effect in src/ is grepped for state it drops",
		Files:      "src/Reader.tsx",
	}
}

func TestATesterFilesANewFindingHeldUnderTheLandedStorysEpic(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	var out bytes.Buffer
	filer := application.TesterFiler{Tracker: tracker, Rules: rules, Out: &out}

	filed, err := filer.File(context.Background(), "mw-e.2", testerFinding())
	if err != nil {
		t.Fatal(err)
	}
	if filed.Duplicate != "" || filed.ID == "" {
		t.Fatalf("expected a new story, got %+v", filed)
	}

	story, err := tracker.ShowStory(context.Background(), filed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if story.Status != apptest.StatusDeferred {
		t.Errorf("expected the story held, got %q", story.Status)
	}
	if story.EpicID != "mw-e" {
		t.Errorf("expected it under the landed story's epic mw-e, got %q", story.EpicID)
	}
	if want := "[bug] lampas: The page number is lost after rotate"; story.Story.Title != want {
		t.Errorf("expected the title %q, got %q", want, story.Story.Title)
	}
	path := story.Merged()
	if path.Rig != "lampas" || path.Branch != "main" || path.Model != domain.ModelSonnet || path.Effort != domain.EffortHigh ||
		path.Host != domain.HostAuto || path.Formula != "tdd-feature" || path.Harness != domain.HarnessClaude {
		t.Errorf("expected the landed story's path with host auto, got %+v", path)
	}
	if path.Files != "src/Reader.tsx" {
		t.Errorf("expected the files key src/Reader.tsx, got %q", path.Files)
	}
	if !domain.IsBugStory(story.Type, story.Story.Title) {
		t.Error("expected a [bug] story to be a bug story")
	}
	for _, want := range []string{
		"Rotate to 844x390 and back: the Reader is on page 1 again.",
		"Tester finding",
		"Tester on mw-e.1 (mw-e.2)",
		"/tmp/tester-mw-e.2/06-rotate.png",
		"PROVISIONAL (Tester's suggestion; Mayor to confirm): keep the page in state, not in the viewport effect",
		"HOW TO CHECK IT",
		"2. Rotate the phone and back: page 7 is still shown.",
	} {
		if !strings.Contains(story.Description, want) {
			t.Errorf("expected the description to hold %q, got:\n%s", want, story.Description)
		}
	}
	if !strings.Contains(story.Acceptance, "npx vitest run src/Reader.test.tsx") {
		t.Errorf("expected the acceptance given, got %q", story.Acceptance)
	}

	// Waits on the running and the open story that name src/Reader.tsx, and on
	// no closed, held, or other-file story.
	needs := needsOf(t, tracker, filed.ID)
	slices.Sort(needs)
	if !slices.Equal(needs, []string{"mw-e.4", "mw-e.5"}) {
		t.Errorf("expected the new story to wait on mw-e.4 and mw-e.5, got %q", needs)
	}
	if !slices.Equal(filed.Waits, []string{"mw-e.4", "mw-e.5"}) {
		t.Errorf("expected the result to name them, got %q", filed.Waits)
	}
	// The epic's open demo waits on it.
	if demo := needsOf(t, tracker, "mw-e.3"); !slices.Contains(demo, filed.ID) {
		t.Errorf("expected the demo mw-e.3 to wait on %s, got %q", filed.ID, demo)
	}
	if !strings.Contains(out.String(), filed.ID) || !strings.Contains(out.String(), "held") {
		t.Errorf("expected the output to say the held id, got %q", out.String())
	}
}

func TestATesterNamesTheOpenBeadAFindingDuplicatesAndFilesNothing(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	var out bytes.Buffer
	filer := application.TesterFiler{Tracker: tracker, Rules: rules, Out: &out}
	before := len(tracker.Stories())

	for _, title := range []string{"The mic stays red after Back", "the mic  stays red after back."} {
		finding := testerFinding()
		finding.Title = title
		filed, err := filer.File(context.Background(), "mw-e.2", finding)
		if err != nil {
			t.Fatal(err)
		}
		if filed.ID != "" || filed.Duplicate != "mw-e.7" {
			t.Errorf("%q: expected no story and mw-e.7 named, got %+v", title, filed)
		}
	}
	if after := len(tracker.Stories()); after != before {
		t.Errorf("expected no story filed, %d became %d", before, after)
	}
	if !strings.Contains(out.String(), "mw-e.7") || !strings.Contains(out.String(), "not filed") {
		t.Errorf("expected the output to name mw-e.7 as the duplicate, got %q", out.String())
	}
}

func TestAClosedBeadIsNoDuplicateOfAFinding(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	if err := tracker.SetStatus("mw-e.7", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	finding := testerFinding()
	finding.Title = "The mic stays red after Back"
	filed, err := application.TesterFiler{Tracker: tracker, Rules: rules}.File(context.Background(), "mw-e.2", finding)
	if err != nil {
		t.Fatal(err)
	}
	if filed.ID == "" || filed.Duplicate != "" {
		t.Errorf("expected a new story, got %+v", filed)
	}
}

func TestATasteFindingIsFiledTastedAndNeedsNoFilesKey(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	finding := testerFinding()
	finding.Kind, finding.Files = "taste", ""
	// A taste finding is no bug, so it owes no Sweep line (mw-gq6.447).
	finding.Acceptance = "npx vitest run src/Reader.test.tsx passes with the new rotate case"
	filed, err := application.TesterFiler{Tracker: tracker, Rules: rules}.File(context.Background(), "mw-e.2", finding)
	if err != nil {
		t.Fatal(err)
	}
	story, err := tracker.ShowStory(context.Background(), filed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[taste] lampas: The page number is lost after rotate"; story.Story.Title != want {
		t.Errorf("expected the title %q, got %q", want, story.Story.Title)
	}
	if domain.IsBugStory(story.Type, story.Story.Title) {
		t.Error("expected a [taste] story not to be a bug story")
	}
	if story.Merged().Files != "" || len(filed.Waits) != 0 {
		t.Errorf("expected no files key and no same-file wait, got %q and %q", story.Merged().Files, filed.Waits)
	}
}

func TestTesterFilingUsesHostAutoUnlessTheTesterNamesOne(t *testing.T) {
	for name, tc := range map[string]struct{ host, want string }{
		"no host named": {"", domain.HostAuto},
		"a host named":  {"cloud2", "cloud2"},
	} {
		tracker, rules := testerFileEpic(t)
		finding := testerFinding()
		finding.Host = tc.host
		filed, err := application.TesterFiler{Tracker: tracker, Rules: rules}.File(context.Background(), "mw-e.2", finding)
		if err != nil {
			t.Fatal(err)
		}
		story, err := tracker.ShowStory(context.Background(), filed.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := story.Merged().Host; got != tc.want {
			t.Errorf("%s: the landed story ran on laptop; expected host %q, got %q", name, tc.want, got)
		}
		if story.Merged().Rig != "lampas" || story.Merged().Formula != "tdd-feature" {
			t.Errorf("%s: expected the rest of the landed story's path kept, got %+v", name, story.Merged())
		}
	}
}

func TestTesterFilingPutsABugAtP2AndATasteFindingAtP3(t *testing.T) {
	for kind, want := range map[string]int{application.TesterKindBug: 2, application.TesterKindTaste: 3} {
		tracker, rules := testerFileEpic(t)
		finding := testerFinding()
		finding.Kind = kind
		filed, err := application.TesterFiler{Tracker: tracker, Rules: rules}.File(context.Background(), "mw-e.2", finding)
		if err != nil {
			t.Fatal(err)
		}
		story, err := tracker.ShowStory(context.Background(), filed.ID)
		if err != nil {
			t.Fatal(err)
		}
		if story.Priority != want {
			t.Errorf("%s: expected priority %d, got %d", kind, want, story.Priority)
		}
	}
}

func TestTesterFilingChainsFindingsThatNameTheSameFileInFilingOrder(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	var out bytes.Buffer
	filer := application.TesterFiler{Tracker: tracker, Rules: rules, Out: &out}

	first := testerFinding()
	first.Files = "src/Rotate.tsx"
	firstFiled, err := filer.File(context.Background(), "mw-e.2", first)
	if err != nil {
		t.Fatal(err)
	}
	second := testerFinding()
	second.Title = "The page number jumps after a swipe"
	second.Files = "src/Rotate.tsx,src/swipe.ts"
	secondFiled, err := filer.File(context.Background(), "mw-e.2", second)
	if err != nil {
		t.Fatal(err)
	}
	third := testerFinding()
	third.Title = "The settings title is cut off"
	third.Files = "src/Settings.tsx"
	thirdFiled, err := filer.File(context.Background(), "mw-e.2", third)
	if err != nil {
		t.Fatal(err)
	}
	// Held, so the earlier finding is neither open nor running; the later one
	// waits on it all the same.
	if needs := needsOf(t, tracker, firstFiled.ID); slices.Contains(needs, secondFiled.ID) {
		t.Errorf("the earlier finding must not wait on the later: %q", needs)
	}
	if needs := needsOf(t, tracker, secondFiled.ID); !slices.Equal(needs, []string{firstFiled.ID}) {
		t.Errorf("expected the later finding to wait on %s, got %q", firstFiled.ID, needs)
	}
	if !slices.Equal(secondFiled.Chained, []string{firstFiled.ID}) {
		t.Errorf("expected the result to name %s as chained, got %q", firstFiled.ID, secondFiled.Chained)
	}
	// mw-e.4 is running and names no file the second finding does; the third
	// names Settings.tsx, which only the open mw-e.6 names.
	if needs := needsOf(t, tracker, thirdFiled.ID); !slices.Equal(needs, []string{"mw-e.6"}) || len(thirdFiled.Chained) != 0 {
		t.Errorf("expected the third to wait on mw-e.6 only, got %q and chained %q", needs, thirdFiled.Chained)
	}
	if !strings.Contains(out.String(), "waits on "+firstFiled.ID) {
		t.Errorf("expected the output to say the chain, got %q", out.String())
	}
}

func TestTesterFilingDoesNotChainToAFindingOfAnotherLanding(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.20", Title: "[bug] lampas: Old finding", Overrides: domain.Path{Files: "src/Rotate.tsx"}})
	if err := tracker.SetDescription("mw-e.20", "x\n\nTester finding. Source: Tester on mw-e.19 (mw-e.18).\n"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStatus("mw-e.20", apptest.StatusDeferred); err != nil {
		t.Fatal(err)
	}
	finding := testerFinding()
	finding.Files = "src/Rotate.tsx"
	filed, err := application.TesterFiler{Tracker: tracker, Rules: rules}.File(context.Background(), "mw-e.2", finding)
	if err != nil {
		t.Fatal(err)
	}
	if len(filed.Chained) != 0 {
		t.Errorf("expected no chain to a finding of another landing, got %q", filed.Chained)
	}
}

func TestATesterFindingIsRefusedWithoutWhatTheMayorNeeds(t *testing.T) {
	for name, change := range map[string]func(*application.TesterFinding){
		"no kind":       func(f *application.TesterFinding) { f.Kind = "" },
		"a wrong kind":  func(f *application.TesterFinding) { f.Kind = "nit" },
		"no title":      func(f *application.TesterFinding) { f.Title = "  " },
		"no finding":    func(f *application.TesterFinding) { f.Finding = "" },
		"no acceptance": func(f *application.TesterFinding) { f.Acceptance = "" },
		"no how":        func(f *application.TesterFinding) { f.HowToCheck = "" },
		"a bug with no sweep": func(f *application.TesterFinding) {
			f.Acceptance = "npx vitest run src/Reader.test.tsx passes\nSweep listed in the closing comment"
		},
		"a bug whose sweep is none with no reason": func(f *application.TesterFinding) {
			f.Acceptance = "npx vitest run src/Reader.test.tsx passes\n2. Sweep: none"
		},
	} {
		tracker, rules := testerFileEpic(t)
		finding := testerFinding()
		change(&finding)
		before := len(tracker.Stories())
		if _, err := (application.TesterFiler{Tracker: tracker, Rules: rules}).File(context.Background(), "mw-e.2", finding); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
		if after := len(tracker.Stories()); after != before {
			t.Errorf("%s: expected nothing filed", name)
		}
	}
}

func TestOnlyATesterStoryNamingALandedStoryCanFile(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	filer := application.TesterFiler{Tracker: tracker, Rules: rules}
	for _, id := range []string{"mw-e.4", "mw-e.99"} {
		if _, err := filer.File(context.Background(), id, testerFinding()); err == nil {
			t.Errorf("%s: expected a refusal", id)
		}
	}
}

func TestAFindingDemoThatIsRunningIsLeftAloneWithANote(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	if err := tracker.SetStatus("mw-e.3", apptest.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	filed, err := application.TesterFiler{Tracker: tracker, Rules: rules}.File(context.Background(), "mw-e.2", testerFinding())
	if err != nil {
		t.Fatal(err)
	}
	if len(filed.Notes) != 1 || !strings.Contains(filed.Notes[0], "mw-e.3") {
		t.Errorf("expected one note naming the demo, got %q", filed.Notes)
	}
}

func TestReadTesterFindingsListsTheHeldStoriesAndTheDuplicatesNamed(t *testing.T) {
	found, ok := application.ReadTesterFindings("FINDINGS\n" +
		"- [bug] Page lost after rotate. Shot: /tmp/a.png. still on main: yes\n  held: mw-e.10\n" +
		"- [bug] The mic stays red. still on main: yes\n  duplicate of: mw-e.7 (not filed)\n" +
		"- [taste] The list jumps. still on main: no (0.4.2)\n  held: mw-e.11, mw-e.12\n" +
		"Tester fuel: unknown\n  held: mw-e.99")
	if !ok {
		t.Fatal("expected the FINDINGS to be read")
	}
	if want := []string{"mw-e.10", "mw-e.11", "mw-e.12"}; !slices.Equal(found.Held, want) {
		t.Errorf("expected the held ids %q, got %q", want, found.Held)
	}
	if want := []string{"mw-e.7"}; !slices.Equal(found.Duplicates, want) {
		t.Errorf("expected the duplicates %q, got %q", want, found.Duplicates)
	}
	if found.Bug != 2 || found.Taste != 1 {
		t.Errorf("expected 2 bug and 1 taste, got %+v", found)
	}
}

// The Tested mail for mw-8m9dyu.6 listed 'B-hidden, tester-mw, re-runs' as held
// stories beside the four real ones (mw-gq6.541).
func TestReadTesterFindingsHeldNamesOnlyRealBeadIdsOfTheRigPrefix(t *testing.T) {
	found, ok := application.ReadTesterFindings("FINDINGS\n" +
		"- [bug] Page lost after rotate. still on main: yes\n" +
		"  held: mw-8m9dyu.10, mw-8m9dyu.11, mw-8m9dyu.12, mw-8m9dyu.13 B-hidden, tester-mw, re-runs\n")
	if !ok {
		t.Fatal("expected the FINDINGS to be read")
	}
	want := []string{"mw-8m9dyu.10", "mw-8m9dyu.11", "mw-8m9dyu.12", "mw-8m9dyu.13"}
	if !slices.Equal(found.Held, want) {
		t.Errorf("expected the held ids %q, got %q", want, found.Held)
	}
}

func TestReadTesterFindingsNeverTakesAHyphenatedWordForAnId(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		held, dups []string
	}{
		{"held words alone", "held: B-hidden, tester-mw, re-runs", nil, nil},
		{"held id of another rig's prefix", "held: lp-12", nil, nil},
		{"held id then an id of another rig's prefix", "held: mw-e.4, lp-12", []string{"mw-e.4"}, nil},
		{"held word ending in the prefix", "held: tester-mw", nil, nil},
		{"held word with the prefix inside it", "held: re-mw-9", nil, nil},
		{"held ids then a parenthesis naming words and ids", "held: mw-e.4 (B-hidden, tester-mw, also mw-e.9)", []string{"mw-e.4"}, nil},
		{"held ids then prose naming another id", "held: mw-e.4, mw-e.5 because mw-e.1 was in the way", []string{"mw-e.4", "mw-e.5"}, nil},
		{"duplicate of a word", "duplicate of: re-runs", nil, nil},
		{"duplicate of a word then an id in prose", "duplicate of: B-hidden, see mw-e.7", nil, []string{"mw-e.7"}},
		{"duplicate of an id then words", "duplicate of: mw-e.7 tester-mw", nil, []string{"mw-e.7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, ok := application.ReadTesterFindings("FINDINGS\n- [bug] A thing. still on main: yes\n  " + tc.line + "\n")
			if !ok {
				t.Fatal("expected the FINDINGS to be read")
			}
			if !slices.Equal(found.Held, tc.held) {
				t.Errorf("expected the held ids %q, got %q", tc.held, found.Held)
			}
			if !slices.Equal(found.Duplicates, tc.dups) {
				t.Errorf("expected the duplicates %q, got %q", tc.dups, found.Duplicates)
			}
		})
	}
}

// A Sweep line written in the finding lands in the story's description, which
// dispatch does not read: the refusal says to put it in --accept (mw-gq6.548).
func TestATesterBugWithItsSweepInTheFindingIsToldItBelongsInAccept(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	finding := testerFinding()
	finding.Finding += "\nSweep: every viewport effect in src/ is grepped for state it drops"
	finding.Acceptance = "npx vitest run src/Reader.test.tsx passes"
	_, err := (application.TesterFiler{Tracker: tracker, Rules: rules}).File(context.Background(), "mw-e.2", finding)
	if err == nil || !strings.Contains(err.Error(), "its Sweep line is in the finding, which becomes the description, not in --accept") ||
		strings.Contains(err.Error(), "no line begins") {
		t.Errorf("got %v", err)
	}

	finding.Finding = testerFinding().Finding
	_, err = (application.TesterFiler{Tracker: tracker, Rules: rules}).File(context.Background(), "mw-e.2", finding)
	if err == nil || !strings.Contains(err.Error(), "no line begins with 'Sweep:'") || strings.Contains(err.Error(), "in the finding") {
		t.Errorf("a finding with no Sweep line anywhere keeps today's text, got %v", err)
	}
}

// classFinding is a double-tap finding of the error class tap-settle on a screen.
func classFinding(screen string) application.TesterFinding {
	finding := testerFinding()
	finding.Title = "Double tap on " + screen + " files twice"
	finding.Finding = "Tap Save twice fast on " + screen + ": two entries."
	finding.Shot = "/tmp/tester-mw-e.2/05-" + screen + ".png"
	finding.HowToCheck = "1. Open " + screen + ".\n2. Tap Save twice fast: one entry."
	finding.Class = "tap-settle"
	return finding
}

func TestASecondFindingOfTheSameClassAndAppJoinsTheFirst(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	var out bytes.Buffer
	filer := application.TesterFiler{Tracker: tracker, Rules: rules, Out: &out}

	first, err := filer.File(context.Background(), "mw-e.2", classFinding("Settings"))
	if err != nil || first.ID == "" {
		t.Fatalf("expected the first finding filed, got %+v, %v", first, err)
	}
	story, err := tracker.ShowStory(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(story.Labels, "class:tap-settle") || !slices.Contains(story.Labels, application.LabelBug) {
		t.Errorf("expected the labels bug and class:tap-settle, got %q", story.Labels)
	}
	before := len(tracker.Stories())

	second := classFinding("Library")
	second.Acceptance = "npx vitest run src/Library.test.tsx passes\nSweep: every Save button is grepped for a settle"
	joined, err := filer.File(context.Background(), "mw-e.2", second)
	if err != nil {
		t.Fatal(err)
	}
	if joined.ID != "" || joined.Joined != first.ID {
		t.Fatalf("expected nothing filed and %s joined, got %+v", first.ID, joined)
	}
	if after := len(tracker.Stories()); after != before {
		t.Errorf("expected no story filed, %d became %d", before, after)
	}
	comments := tracker.Comments(first.ID)
	if len(comments) != 1 {
		t.Fatalf("expected one comment on %s, got %q", first.ID, comments)
	}
	for _, want := range []string{
		"Also on: Double tap on Library files twice",
		"Tap Save twice fast on Library: two entries.",
		"/tmp/tester-mw-e.2/05-Library.png",
		"2. Tap Save twice fast: one entry.",
		"npx vitest run src/Library.test.tsx passes",
		"Sweep: every Save button is grepped for a settle",
	} {
		if !strings.Contains(comments[0], want) {
			t.Errorf("expected the comment to hold %q, got:\n%s", want, comments[0])
		}
	}
	if !strings.Contains(out.String(), "joined: "+first.ID) {
		t.Errorf("expected the output to say 'joined: %s', got %q", first.ID, out.String())
	}
}

func TestAFindingOfAnotherAppOrClassIsFiledNew(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	filer := application.TesterFiler{Tracker: tracker, Rules: rules}
	first, err := filer.File(context.Background(), "mw-e.2", classFinding("Settings"))
	if err != nil {
		t.Fatal(err)
	}

	otherApp := classFinding("Library")
	otherApp.App = "otherapp"
	filedApp, err := filer.File(context.Background(), "mw-e.2", otherApp)
	if err != nil {
		t.Fatal(err)
	}
	otherClass := classFinding("Search")
	otherClass.Class = "stale-closure"
	filedClass, err := filer.File(context.Background(), "mw-e.2", otherClass)
	if err != nil {
		t.Fatal(err)
	}
	noClass := classFinding("Help")
	noClass.Class = ""
	filedNone, err := filer.File(context.Background(), "mw-e.2", noClass)
	if err != nil {
		t.Fatal(err)
	}
	for name, filed := range map[string]application.TesterFiled{"app": filedApp, "class": filedClass, "no class": filedNone} {
		if filed.ID == "" || filed.Joined != "" || filed.ID == first.ID {
			t.Errorf("another %s: expected a new story, got %+v", name, filed)
		}
	}
	if comments := tracker.Comments(first.ID); len(comments) != 0 {
		t.Errorf("expected no comment on %s, got %q", first.ID, comments)
	}
	story, err := tracker.ShowStory(context.Background(), filedNone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(story.Labels, func(l string) bool { return strings.HasPrefix(l, "class:") }) {
		t.Errorf("expected no class label without --class, got %q", story.Labels)
	}
}

func TestAClosedStoryOfTheClassIsNeverJoined(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	filer := application.TesterFiler{Tracker: tracker, Rules: rules}
	first, err := filer.File(context.Background(), "mw-e.2", classFinding("Settings"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStatus(first.ID, apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	second, err := filer.File(context.Background(), "mw-e.2", classFinding("Library"))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == "" || second.Joined != "" || second.ID == first.ID {
		t.Errorf("expected a new story, got %+v", second)
	}
	if comments := tracker.Comments(first.ID); len(comments) != 0 {
		t.Errorf("expected no comment on the closed story, got %q", comments)
	}
}

func TestAnOpenStoryOfTheClassIsJoinedToo(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	filer := application.TesterFiler{Tracker: tracker, Rules: rules}
	first, err := filer.File(context.Background(), "mw-e.2", classFinding("Settings"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.ReleaseStory(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := filer.File(context.Background(), "mw-e.2", classFinding("Library"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Joined != first.ID {
		t.Errorf("expected %s joined, got %+v", first.ID, second)
	}
}

type classRegister map[string]string

func (r classRegister) ReadErrorRegister(context.Context, string) (map[string]string, string, error) {
	return r, "", nil
}
func (r classRegister) WriteErrorClass(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (r classRegister) WriteErrorKinds(context.Context, string, string) (string, error) {
	return "", nil
}

func TestAClassNotInTheRegisterIsRefused(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	filer := application.TesterFiler{Tracker: tracker, Rules: rules, Register: classRegister{"tap-settle": "x"}, Seat: "builder"}
	before := len(tracker.Stories())
	bad := classFinding("Settings")
	bad.Class = "tap-setle"
	_, err := filer.File(context.Background(), "mw-e.2", bad)
	if err == nil || !strings.Contains(err.Error(), "mw errors list") {
		t.Fatalf("expected a refusal that points at mw errors list, got %v", err)
	}
	if len(tracker.Stories()) != before {
		t.Error("expected nothing filed")
	}
	if _, err := filer.File(context.Background(), "mw-e.2", classFinding("Settings")); err != nil {
		t.Errorf("expected a class of the register to file, got %v", err)
	}
	notSlug := classFinding("Settings")
	notSlug.Class = "Tap Settle"
	if _, err := (application.TesterFiler{Tracker: tracker, Rules: rules}).File(context.Background(), "mw-e.2", notSlug); err == nil {
		t.Error("expected a class that is no slug to be refused")
	}
}
