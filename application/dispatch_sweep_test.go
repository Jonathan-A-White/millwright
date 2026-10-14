package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aBugFiledAt files a ready bug of the factory's epic at created, with the
// acceptance given.
func aBugFiledAt(t *testing.T, tracker *apptest.FakeTracker, id string, created time.Time, acceptance string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "[bug] the header is missing on one route"})
	if err := tracker.SetCreated(id, created); err != nil {
		t.Fatalf("dating %s: %v", id, err)
	}
	if err := tracker.SetAcceptance(id, acceptance); err != nil {
		t.Fatalf("writing the acceptance of %s: %v", id, err)
	}
}

var afterTheRule = domain.SweepRuleFrom.Add(24 * time.Hour)

func TestADispatchDoesNotStartANewBugWithNoSweepAndTellsTheMayorOnce(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Mailbox = mailbox
	aBugFiledAt(t, tracker, "mw-gq6.1", afterTheRule, "1. The header is on the route.")

	for tick := 1; tick <= 3; tick++ {
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if len(report.Started) != 0 {
			t.Fatalf("tick %d: started %v, want nothing", tick, startedIDs(report))
		}
		if len(report.Passed) != 1 || !strings.Contains(report.Passed[0].Why, "Sweep:") {
			t.Fatalf("tick %d: the pass should say why, got %+v", tick, report.Passed)
		}
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "" || d.Status != "open" {
		t.Errorf("the bug should be left open and unclaimed, got status %q assignee %q", d.Status, d.Assignee)
	}
	if len(worktrees.added) != 0 || len(runner.Started()) != 0 {
		t.Errorf("a worktree or session was made: %v %v", worktrees.added, runner.Started())
	}
	if got := mailbox.Writes(); got != 1 {
		t.Fatalf("the Mayor should be mailed once over three ticks, got %d", got)
	}
	inbox, _ := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "mw-gq6.1") || !strings.Contains(inbox[0].Body, "Sweep: none, because") {
		t.Errorf("the mail should name the bead and the escape, got %+v", inbox)
	}
	if comments := tracker.Comments("mw-gq6.1"); len(comments) != 1 || !strings.Contains(comments[0], application.ReasonNoSweep) {
		t.Errorf("one comment should say it, got %q", comments)
	}
}

func TestADispatchStartsABugWhoseAcceptanceCarriesASweep(t *testing.T) {
	for name, acceptance := range map[string]string{
		"a sweep":           "1. It passes.\n2. Sweep: grep every route for the header.",
		"none with because": "1. It passes.\n2. Sweep: none, because this is the rule itself.",
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			aBugFiledAt(t, tracker, "mw-gq6.1", afterTheRule, acceptance)
			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
				t.Errorf("started %v, want the bug", ids)
			}
		})
	}
}

func TestADispatchDoesNotStartABugWhoseSweepIsNoneWithNoReason(t *testing.T) {
	for _, acceptance := range []string{"Sweep: none", "Sweep: none, because", "Sweep:"} {
		dispatch, tracker, _, _, _ := aFactory(t)
		aBugFiledAt(t, tracker, "mw-gq6.1", afterTheRule, "1. It passes.\n"+acceptance)
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("dispatching: %v", err)
		}
		if len(report.Started) != 0 {
			t.Errorf("%q: started %v, want nothing", acceptance, startedIDs(report))
		}
	}
}

func TestADispatchStillStartsABugFiledBeforeTheRule(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Mailbox = apptest.NewFakeMailbox()
	aBugFiledAt(t, tracker, "mw-gq6.1", domain.SweepRuleFrom.Add(-time.Hour), "1. The header is on the route.")
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 {
		t.Errorf("started %v, want the older bug", ids)
	}
}

func TestADispatchStartsAStoryThatIsNotABugWithNoSweep(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A feature"})
	_ = tracker.SetCreated("mw-gq6.1", afterTheRule)
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 {
		t.Errorf("started %v, want the feature", ids)
	}
}

func TestADryRunDispatchPassesOverASweeplessBugAndWritesNothing(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Mailbox, dispatch.DryRun = mailbox, true
	aBugFiledAt(t, tracker, "mw-gq6.1", afterTheRule, "1. It passes.")
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Passed) != 1 || mailbox.Writes() != 0 || len(tracker.Comments("mw-gq6.1")) != 0 {
		t.Errorf("a dry run says the pass and writes nothing: %+v, %d mails, %q", report.Passed, mailbox.Writes(), tracker.Comments("mw-gq6.1"))
	}
}
