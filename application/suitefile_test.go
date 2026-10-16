package application_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestASuiteBugFiledIntoTheSuiteEpicIsANeedOfTheEpicsDemo(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-s", domain.Path{Rig: "lampas"})
	tracker.AddStory("mw-s", domain.Story{ID: "mw-s.1", Title: "Show the suite"})
	if err := tracker.SetLabels("mw-s.1", application.LabelDemo); err != nil {
		t.Fatal(err)
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{LastStoryLabels: []string{"demo"}, SuiteEpic: "mw-s"})
	report := application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234",
		Failing: []application.SuiteFile{{File: "src/a.test.ts"}}}}

	filer := application.SuiteFiler{Tracker: tracker, Rules: rules, Mailbox: apptest.NewFakeMailbox(), Host: "desktop"}
	filed, err := filer.File(context.Background(), "mw-s", report)
	if err != nil || len(filed) != 1 || !filed[0].New {
		t.Fatalf("expected one new bug, got %+v, %v", filed, err)
	}
	epic, err := tracker.ShowEpic(context.Background(), "mw-s")
	if err != nil {
		t.Fatal(err)
	}
	for _, story := range epic.Stories {
		if story.Story.ID == "mw-s.1" && !slices.Contains(story.Needs, filed[0].ID) {
			t.Errorf("expected the demo to wait on the bug %s, got %q", filed[0].ID, story.Needs)
		}
	}
}
