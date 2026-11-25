package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1609: lampas story mw-qtu3zvz.24's branch was never rebased on a story
// that had landed, so it conflicted with main itself. Batches 611, 612 and 613
// each set it aside "it conflicts with the batch" and kept it run=queued, its
// comment promising a fixer that a batch with other merging stories never gives.
// A story that conflicts with main alone is handed back at once.

const withMain = "conflicts with main, not the batch"

// handedBack fails the test unless story id was taken off the batch queue, its
// claim given back, with a comment on it naming each of want.
func handedBack(t *testing.T, a aSetAsideBatch, id string, want ...string) {
	t.Helper()
	if got := a.tracker.State(id, application.RunState); got == application.RunQueued {
		t.Errorf("expected %s taken off the batch queue, got run=%q", id, got)
	}
	detail, err := a.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "" || detail.Status != application.StatusOpen {
		t.Errorf("expected the claim on %s given back, the story open for a Builder, got status %q held by %q", id, detail.Status, detail.Assignee)
	}
	comments := strings.Join(a.tracker.Comments(id), "\n---\n")
	for _, w := range want {
		if !strings.Contains(comments, w) {
			t.Errorf("expected a comment on %s naming %q, got:\n%s", id, w, comments)
		}
	}
}

// notSaidToStayQueued fails the test when a comment on story id says it stays queued.
func notSaidToStayQueued(t *testing.T, a aSetAsideBatch, id string) {
	t.Helper()
	if comments := strings.Join(a.tracker.Comments(id), "\n---\n"); strings.Contains(comments, "stays queued") {
		t.Errorf("expected no comment saying %s stays queued, got:\n%s", id, comments)
	}
}

// theBatchMail sends the batch's one mail and returns its body.
func theBatchMail(t *testing.T, a aSetAsideBatch, mailbox *apptest.FakeMailbox) string {
	t.Helper()
	a.land.MailTheBatch(context.Background(), a.report)
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected one mail to the Mayor, got %d", len(inbox))
	}
	return inbox[0].Body
}

func TestAStoryThatConflictsWithMainAloneIsHandedBackAtOnce(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 3, "mw-e.2"))
	mailbox := apptest.NewFakeMailbox()
	a.land.Next.Mailbox = mailbox
	a.run(t)

	if !a.report.Landed || len(a.report.Merged) != 2 {
		t.Fatalf("expected the other two landed, got landed %v, merged %d, held %q", a.report.Landed, len(a.report.Merged), a.report.Held)
	}
	handedBack(t, a, "mw-e.2", withMain, "ebase", "src/handsFree/session.ts")
	notSaidToStayQueued(t, a, "mw-e.2")
	if len(a.report.SetAside) != 1 || !a.report.SetAside[0].HandedBack {
		t.Fatalf("expected mw-e.2 handed back in the report, got %+v", a.report.SetAside)
	}
	if got := a.tracker.Metadata("mw-e.2")[application.BatchConflictsField]; got != "1" {
		t.Errorf("expected the set-aside still counted on the story, got %q", got)
	}
	if mail := theBatchMail(t, a, mailbox); !strings.Contains(mail, withMain) {
		t.Errorf("expected the mail to say %q, got:\n%s", withMain, mail)
	}
}

func TestTheFirstStoryOfABatchThatConflictsIsHandedBackAsConflictingWithMain(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 3, "mw-e.1"))
	a.run(t)
	handedBack(t, a, "mw-e.1", withMain)
	notSaidToStayQueued(t, a, "mw-e.1")
}

func TestAStoryThatConflictsOnlyWithItsBatchMatesStaysQueued(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 3, "mw-e.2"))
	a.landing.onlyBesideMates("mw-e.2")
	a.run(t)

	if got := a.tracker.State("mw-e.2", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-e.2 left queued, a conflict with its batch mates may clear once they have landed, got run=%q", got)
	}
	if why := a.report.SetAside[0].Why; !strings.Contains(why, "conflicts with the batch") || strings.Contains(why, withMain) {
		t.Errorf("expected the reason to be the batch, got %q", why)
	}
}

func TestAStorySetAsideTwiceInARowForTheSameFilesIsHandedBackAtTheSecond(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 3, "mw-e.2"))
	a.landing.onlyBesideMates("mw-e.2")
	a.run(t)
	second := aLaterBatch(a, 596)
	second.run(t)

	handedBack(t, second, "mw-e.2", "src/handsFree/session.ts", "docs/layout/reader.md", "2 batches")
}

// aLandingThatCannotCut is a landing that cannot open a worktree for a trial merge.
type aLandingThatCannotCut struct{ application.Landing }

func (aLandingThatCannotCut) OpenLanding(context.Context, string, string) (string, error) {
	return "", errors.New("no room on the disk")
}

func TestAStoryWhoseTrialMergeOnMainCannotBeMadeStaysQueuedAndIsNoted(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 3, "mw-e.2"))
	a.land.Next.Landing = aLandingThatCannotCut{a.land.Next.Landing}
	a.run(t)

	if got := a.tracker.State("mw-e.2", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-e.2 left queued when main alone could not be tried, got run=%q", got)
	}
	if notes := strings.Join(a.report.Notes, "\n"); !strings.Contains(notes, "no room on the disk") {
		t.Errorf("expected a note that the trial merge could not be made, got %q", a.report.Notes)
	}
}

func TestAnAllConflictBatchGivesItsFixerTheStoriesBeforeAnyIsHandedBack(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 2, "mw-e.1", "mw-e.2"))
	a.run(t)

	if got := a.runner.Started(); len(got) != 1 {
		t.Fatalf("expected the batch given its one fixer, got %v", got)
	}
	// The fixer merged neither (the fake's branches are not in its batch), so both
	// are handed back at once, not left for a later batch.
	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		handedBack(t, a, id, withMain)
	}
}

// A fixer the host kept from starting is given the batch afresh (mw-gq6.472,
// mw-2891blf.19), so its stories wait for it; the second set-aside hands them back.
func TestAStoryOwedAFixerThatCouldNotStartWaitsForItOnce(t *testing.T) {
	a := aQueuedClaim(t, aBatchOf(t, 1, "mw-e.1"))
	a.runner.StartErr = errors.New("tmux is gone")
	a.run(t)

	if got := a.tracker.State("mw-e.1", application.RunState); got != application.RunQueued {
		t.Fatalf("expected mw-e.1 left queued for the fixer still owed to it, got run=%q (held %q)", got, a.report.Held)
	}
	second := aLaterBatch(a, 596)
	second.run(t)
	handedBack(t, second, "mw-e.1", withMain)
}
