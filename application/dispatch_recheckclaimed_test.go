package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1629: a suite bug's re-run on main takes minutes, and another host's
// dispatch may claim the bug before it starts or while it runs. The dispatch
// reads the bug again right before the re-run and again before it writes, and
// writes nothing but a comment to a bug it has not claimed.

// claimedAtFirstShow is a tracker that lets another host claim the story the
// first time it is read by ShowStory: the claim lands after the ready listing
// that offered the story and before anything the dispatch does with it.
type claimedAtFirstShow struct {
	*apptest.FakeTracker
	id, holder string
	done       bool
}

func (c *claimedAtFirstShow) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	if id == c.id && !c.done {
		c.done = true
		if err := c.FakeTracker.ClaimAs(id, c.holder, time.Now().Add(time.Hour)); err != nil {
			return application.StoryDetail{}, err
		}
	}
	return c.FakeTracker.ShowStory(ctx, id)
}

func TestASuiteBugClaimedByAnotherHostBeforeItsRerunIsNotRerunClosedOrMailed(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	tracker.ActAs("mw@vps")
	recheck := &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n"}}
	mail := apptest.NewFakeMailbox()
	dispatch.Rechecks, dispatch.Mailbox = recheck, mail
	dispatch.Tracker = &claimedAtFirstShow{FakeTracker: tracker, id: "mw-gq6.1", holder: "mw@cloud17"}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 0 {
		t.Errorf("the spec was re-run for a bug another host holds: %v", recheck.asked)
	}
	if got := tracker.CloseReason("mw-gq6.1"); got != "" {
		t.Errorf("the bug was closed as %q", got)
	}
	if said := tracker.Comments("mw-gq6.1"); len(said) != 0 {
		t.Errorf("a bug another host holds was commented on: %q", said)
	}
	if lost := lostWrites(t, mail); len(lost) != 0 {
		t.Errorf("a lost write was mailed: %+v", lost)
	}
	if len(report.Started) != 0 || len(report.FixedOnMain) != 0 || len(worktrees.added) != 0 || len(runner.Started()) != 0 {
		t.Errorf("nothing should be started or closed: %+v", report)
	}
}

func TestASuiteBugClaimedByAnotherHostDuringItsRerunGetsACommentNotAClose(t *testing.T) {
	dispatch, tracker, _, runner, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	tracker.ActAs("mw@vps")
	recheck := &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n"}}
	recheck.during = func() {
		if err := tracker.ClaimAs("mw-gq6.1", "mw@cloud17", time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("another host claiming: %v", err)
		}
	}
	mail := apptest.NewFakeMailbox()
	dispatch.Rechecks, dispatch.Mailbox = recheck, mail

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 1 {
		t.Fatalf("the re-run was asked %d times, want 1: %v", len(recheck.asked), recheck.asked)
	}
	if got := tracker.CloseReason("mw-gq6.1"); got != "" {
		t.Errorf("the bug was closed as %q", got)
	}
	said := strings.Join(tracker.Comments("mw-gq6.1"), "\n")
	if !strings.Contains(said, "found passing on main at 9f3c2ab41 by vps") {
		t.Errorf("the comment should say it was found passing on main at the sha, by the host, got %q", said)
	}
	if lost := lostWrites(t, mail); len(lost) != 0 {
		t.Errorf("a lost write was mailed: %+v", lost)
	}
	if len(report.FixedOnMain) != 0 || len(report.Started) != 0 || len(runner.Started()) != 0 {
		t.Errorf("nothing should be closed or started: %+v", report)
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "mw@cloud17" {
		t.Errorf("the holder is %q, want it left as mw@cloud17", d.Assignee)
	}
}

func TestASuiteBugNobodyHoldsIsStillClosedWhenItsSpecPassesOnMain(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n"}}

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.CloseReason("mw-gq6.1"); !strings.HasPrefix(got, "already fixed on main at") {
		t.Errorf("close reason %q, want already fixed on main", got)
	}
}
