package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aTesterStoryReady files a ready Tester story with the description given.
func aTesterStoryReady(t *testing.T, tracker *apptest.FakeTracker, id, description string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: application.TesterTitlePrefix + "The story mw-gq6.5"})
	if err := tracker.SetLabels(id, application.LabelTester); err != nil {
		t.Fatalf("labelling %s: %v", id, err)
	}
	if err := tracker.SetDescription(id, description); err != nil {
		t.Fatalf("describing %s: %v", id, err)
	}
}

// mw-gq6.594: a Tester story with no 'Landed story:' line cannot file a finding,
// so its session is never started.
func TestADispatchDoesNotStartATesterStoryNamingNoLandedStoryAndSaysSoOnce(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Mailbox = mailbox
	aTesterStoryReady(t, tracker, "mw-gq6.1", "Drive it on a phone-sized screen.")

	for tick := 1; tick <= 3; tick++ {
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if len(report.Started) != 0 {
			t.Fatalf("tick %d: started %v, want nothing", tick, startedIDs(report))
		}
		if len(report.Passed) != 1 || report.Passed[0].Gate != "tester-no-landed-story" || !strings.Contains(report.Passed[0].Why, application.TesterLandedLine) {
			t.Fatalf("tick %d: the pass should say why, got %+v", tick, report.Passed)
		}
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "" || d.Status != "open" {
		t.Errorf("the story should be left open and unclaimed, got status %q assignee %q", d.Status, d.Assignee)
	}
	if len(worktrees.added) != 0 || len(runner.Started()) != 0 {
		t.Errorf("a worktree or session was made: %v %v", worktrees.added, runner.Started())
	}
	if comments := tracker.Comments("mw-gq6.1"); len(comments) != 1 || !strings.Contains(comments[0], application.ReasonTesterNoLandedStory) {
		t.Errorf("one comment should say it, got %q", comments)
	}
	if got := mailbox.Writes(); got != 1 {
		t.Fatalf("the Mayor should be mailed once over three ticks, got %d", got)
	}
}

func TestADispatchStartsATesterStoryThatNamesItsLandedStory(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aTesterStoryReady(t, tracker, "mw-gq6.1", application.TesterLandedLine+"mw-gq6.5 (landed on main)\n\nDrive it.")
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Errorf("started %v, want the Tester story", ids)
	}
}
