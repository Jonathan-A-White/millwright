package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1651: a Builder that hands a story back unfixed leaves the formula's
// steps open, and the close-out refused it for open steps and re-opened it for
// the same dead end. A closing comment that begins 'Handed back' holds the story.

const aHandedBackComment = "Handed back, not fixed. The branch's net diff against main is empty.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing"

// anOpenStepsStopWith runs mw next on mw-gq6.9, whose formula is left open and
// which carries comment (when not empty) written after its branch's newest commit.
func anOpenStepsStopWith(t *testing.T, comment string) (application.NextReport, *apptest.FakeTracker, *apptest.FakeMailbox) {
	t.Helper()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddFormula("tdd-feature",
		application.FormulaStep{Title: "Understand {{story}}: {{title}}"},
		application.FormulaStep{Title: "Implement until green"},
		application.FormulaStep{Title: "Write the closing comment"},
	)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	committed := now.Add(-30 * time.Hour)
	lands := &aCommittedLanding{aLandingLanding{fakeRetryLanding{AheadCount: 1}}, committed}
	dispatch.Now, dispatch.Landing = func() time.Time { return now }, lands
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.9", now.Add(-33*time.Hour)))
	if comment != "" {
		mustDo(t, tracker.CommentOnStoryAt("mw-gq6.9", comment, committed.Add(time.Hour)))
	}
	runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 1)
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("expected the stale claim to be given back, got %v", err)
	}

	vault := newFakeVault()
	vault.written["mw-gq6.9/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	mailbox := apptest.NewFakeMailbox()
	next := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{}, Mailbox: mailbox,
		Seat: "builder", Host: "vps",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}
	report, _ := next.Run(context.Background(), "mw-gq6.9")
	return report, tracker, mailbox
}

func theMayorsSubjects(t *testing.T, mailbox *apptest.FakeMailbox) []string {
	t.Helper()
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	mustDo(t, err)
	var subjects []string
	for _, message := range inbox {
		subjects = append(subjects, message.Subject)
	}
	return subjects
}

func TestOpenStepsStopWithAHandedBackClosingCommentIsMailedHandedBack(t *testing.T) {
	report, tracker, mailbox := anOpenStepsStopWith(t, aHandedBackComment)

	subjects := theMayorsSubjects(t, mailbox)
	if len(subjects) != 1 || !strings.HasPrefix(subjects[0], application.MailHandedBack+": ") {
		t.Fatalf("expected one mail whose subject begins %q, got %q", application.MailHandedBack+": ", subjects)
	}
	if got := statusOf(t, tracker, "mw-gq6.9"); got != application.StatusHeld {
		t.Errorf("expected the story held, got %q", got)
	}
	var held string
	for _, comment := range tracker.Comments("mw-gq6.9") {
		if strings.HasPrefix(comment, application.HeldCommentPrefix) {
			held = comment
		}
	}
	if held == "" {
		t.Fatalf("expected a %s comment, got:\n%s", application.HeldCommentPrefix, strings.Join(tracker.Comments("mw-gq6.9"), "\n---\n"))
	}
	for _, want := range []string{"hand", "--met-by-mayor", "--rescoped-by-mayor"} {
		if !strings.Contains(held, want) {
			t.Errorf("expected the held comment to say %q, got:\n%s", want, held)
		}
	}
	if report.Landed {
		t.Errorf("expected nothing landed, got %+v", report)
	}
}

func TestHandedBackClosingCommentIsReadInAnyLetterCase(t *testing.T) {
	_, _, mailbox := anOpenStepsStopWith(t, "\n  handed back: the story is wrong.\n\nFor the rig memory:\nnothing")
	if subjects := theMayorsSubjects(t, mailbox); len(subjects) != 1 || !strings.HasPrefix(subjects[0], application.MailHandedBack+": ") {
		t.Fatalf("expected a hand-back mail, got %q", subjects)
	}
}

func TestHandedBackWordsDeepInAnOrdinaryClosingCommentAreNotAHandBack(t *testing.T) {
	for name, comment := range map[string]string{
		"no comment":         "",
		"ordinary comment":   aMayorsClosingComment,
		"no rig memory":      "Handed back, not fixed.\n\nInternal: nothing for the Governor to look at",
		"not the first line": "Done.\nHanded back to the Mayor is not what happened.\n\nFor the rig memory:\nnothing",
	} {
		report, tracker, mailbox := anOpenStepsStopWith(t, comment)
		if report.Reason != application.ReasonOpenSteps {
			t.Fatalf("%s: expected %s, got %+v", name, application.ReasonOpenSteps, report)
		}
		if subjects := theMayorsSubjects(t, mailbox); len(subjects) != 1 || !strings.HasPrefix(subjects[0], application.MailRefused+": ") {
			t.Errorf("%s: expected a Refused mail, got %q", name, subjects)
		}
		if got := statusOf(t, tracker, "mw-gq6.9"); got != application.StatusOpen {
			t.Errorf("%s: expected the story given back open, got %q", name, got)
		}
	}
}
