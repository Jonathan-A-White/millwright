package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestReadTesterFindingsCountsBugAndTasteUpToTheFuelLine(t *testing.T) {
	for _, tc := range []struct {
		name       string
		text       string
		ok         bool
		bug, taste int
	}{
		{"none", "FINDINGS: none\nTester fuel: 9 tokens", true, 0, 0},
		{"none in bold", "**FINDINGS:** none", true, 0, 0},
		{"two", "Ran it.\n\nFINDINGS\n- [bug] Red after Back.\n  Shot: /tmp/a.png\n- [Taste] Jumps on rotate.\nTester fuel: 9 tokens\n- [bug] not a finding, after the fuel line", true, 1, 1},
		{"a heading", "## FINDINGS\n1. [bug] One.\n2. [bug] Two.", true, 2, 0},
		{"no heading", "The FINDINGS are in the other comment.", false, 0, 0},
	} {
		found, ok := application.ReadTesterFindings(tc.text)
		if ok != tc.ok || found.Bug != tc.bug || found.Taste != tc.taste {
			t.Errorf("%s: got %+v, %v; want %d bug, %d taste, %v", tc.name, found, ok, tc.bug, tc.taste, tc.ok)
		}
	}
}

func TestATesterTrialIsOnForItsRigsUntilItsUntil(t *testing.T) {
	until := time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)
	trial := application.TesterTrial{Rigs: []string{"lampas"}, Until: until, Model: domain.ModelSonnet, Effort: domain.EffortHigh}
	if !trial.On("lampas", until.Add(-time.Minute)) {
		t.Error("expected a lampas landing a minute before until to be in the trial")
	}
	if trial.On("lampas", until) || trial.On("millwright", until.Add(-time.Hour)) {
		t.Error("expected a landing at until, or on a rig not listed, to be outside the trial")
	}
	if (application.TesterTrial{}).On("lampas", until) {
		t.Error("expected no trial to take any landing")
	}
}

func TestATesterStoryNamesTheLandedStoryOnItsOwnLine(t *testing.T) {
	d := application.StoryDetail{Description: "Landed story: mw-l.1 (landed on main of lampas at abc1234)\n\nDrive it."}
	if got := application.TesterLanded(d); got != "mw-l.1" {
		t.Errorf("expected mw-l.1, got %q", got)
	}
	if !application.IsTesterStory(application.StoryDetail{Labels: []string{"tester"}}) ||
		!application.IsTesterStory(application.StoryDetail{Story: domain.Story{Overrides: domain.Path{Formula: "tester"}}}) ||
		application.IsTesterStory(application.StoryDetail{}) {
		t.Error("expected a story labelled tester, or worked by the tester formula, and only those, to be a Tester story")
	}
}

func testerEpic(t *testing.T) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "Build it"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.2", Title: "Show it"})
	if err := tracker.SetLabels("mw-e.2", application.LabelDemo); err != nil {
		t.Fatal(err)
	}
	tracker.Needs("mw-e.2", "mw-e.1")
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.3", Title: "Test: Build it"})
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{LastStoryLabels: []string{"demo"}})
	return tracker, rules
}

func needsOf(t *testing.T, tracker *apptest.FakeTracker, id string) []string {
	t.Helper()
	epic, err := tracker.ShowEpic(context.Background(), "mw-e")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range epic.Stories {
		if s.Story.ID == id {
			return s.Needs
		}
	}
	t.Fatalf("no story %s in the epic", id)
	return nil
}

func TestATesterStoryFiledUnderAnEpicIsANeedOfItsOpenDemo(t *testing.T) {
	tracker, rules := testerEpic(t)
	notes := application.HoldDemosForTester(context.Background(), tracker, rules, "lampas", "mw-e", "mw-e.3")
	if len(notes) != 0 {
		t.Errorf("expected no notes, got %q", notes)
	}
	needs := needsOf(t, tracker, "mw-e.2")
	if !slices.Contains(needs, "mw-e.3") || !slices.Contains(needs, "mw-e.1") {
		t.Errorf("expected the demo to wait on mw-e.1 and the Tester mw-e.3, got %q", needs)
	}
}

func TestADemoAlreadyInProgressOrClosedGetsNoNewNeedAndANote(t *testing.T) {
	for _, status := range []string{apptest.StatusInProgress, apptest.StatusClosed} {
		tracker, rules := testerEpic(t)
		if err := tracker.SetStatus("mw-e.2", status); err != nil {
			t.Fatal(err)
		}
		notes := application.HoldDemosForTester(context.Background(), tracker, rules, "lampas", "mw-e", "mw-e.3")
		if needs := needsOf(t, tracker, "mw-e.2"); slices.Contains(needs, "mw-e.3") {
			t.Errorf("%s: the demo should not wait on the Tester, got %q", status, needs)
		}
		if len(notes) != 1 || !strings.Contains(notes[0], "mw-e.2") || !strings.Contains(notes[0], status) {
			t.Errorf("%s: expected one note naming the demo and its status, got %q", status, notes)
		}
	}
}

func TestAnEpicWithNoDemoStoryTakesTheTesterWithoutANote(t *testing.T) {
	tracker, rules := testerEpic(t)
	if err := tracker.SetLabels("mw-e.2"); err != nil {
		t.Fatal(err)
	}
	if notes := application.HoldDemosForTester(context.Background(), tracker, rules, "lampas", "mw-e", "mw-e.3"); len(notes) != 0 {
		t.Errorf("expected no notes, got %q", notes)
	}
	if notes := application.HoldDemosForTester(context.Background(), tracker, nil, "lampas", "mw-e", "mw-e.3"); len(notes) != 0 {
		t.Errorf("expected no notes without rules, got %q", notes)
	}
}

func TestADemoThatCannotBeHeldLeavesANote(t *testing.T) {
	tracker, rules := testerEpic(t)
	rules.Err = errors.New("no vault")
	notes := application.HoldDemosForTester(context.Background(), tracker, rules, "lampas", "mw-e", "mw-e.3")
	if len(notes) != 1 || !strings.Contains(notes[0], "no vault") {
		t.Errorf("expected one note carrying the failure, got %q", notes)
	}
}
