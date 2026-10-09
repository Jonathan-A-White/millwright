package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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
