package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1685: a message the follower already applied (a comment on its bead, an
// answer) is not unread: the 'New postern message' notice, mw postern inbox
// --unread-count and mw talk wait's 'N unread' all leave it out, so the Mayor is
// not woken to read nothing new.

func TestUnreadCountLeavesOutAnAppliedCommentAndAnAppliedAnswer(t *testing.T) {
	f := newPartsFixture(t)
	f.addPlain("direct:comment-1", "words already on the bead")
	f.addPlain("direct:answer-1", "B")
	ctx := context.Background()
	if err := f.tracker.SetNote(ctx, application.PosternAppliedKey("direct:comment-1"), "applied comment mw-e.2 txid direct:comment-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.tracker.SetNote(ctx, application.PosternAppliedKey("direct:answer-1"), "applied answer mw-e.3 txid direct:answer-1"); err != nil {
		t.Fatal(err)
	}

	count, err := f.reader(&bytes.Buffer{}).UnreadCount(ctx)
	if err != nil || count != 0 {
		t.Fatalf("expected 0 unread with only an applied comment and answer, got %d, %v", count, err)
	}

	f.addPlain("direct:new", "something new")
	count, err = f.reader(&bytes.Buffer{}).UnreadCount(ctx)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 unread with one new message beside them, got %d, %v", count, err)
	}
}

func TestInboxSaysNothingIsNewWhenEveryMessageIsApplied(t *testing.T) {
	f := newPartsFixture(t)
	f.addPlain("direct:comment-1", "words already on the bead")
	ctx := context.Background()
	if err := f.tracker.SetNote(ctx, application.PosternAppliedKey("direct:comment-1"), "applied comment mw-e.2 txid direct:comment-1"); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	if _, err := f.reader(out).Run(ctx); err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	if !strings.Contains(first, "nothing new") {
		t.Errorf("expected the first line to say nothing is new, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "already applied earlier: applied comment mw-e.2") {
		t.Errorf("expected the applied line to follow, got:\n%s", out.String())
	}

	// With a message not applied, the listing says no such thing.
	f.addPlain("direct:new", "something new")
	out.Reset()
	if _, err := f.reader(out).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "nothing new") || !strings.Contains(out.String(), "something new") {
		t.Errorf("expected the new message and no 'nothing new', got:\n%s", out.String())
	}
}

func TestInboxSaysNothingWhenThereAreNoMessages(t *testing.T) {
	f := newPartsFixture(t)
	out := &bytes.Buffer{}
	if _, err := f.reader(out).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "nothing new") {
		t.Errorf("expected nothing printed for an empty inbox as before, got:\n%s", out.String())
	}
}

func TestTalkWaitDoesNotCountAnAppliedMessage(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addMessage("direct:done-1", "", "his words, already a comment on the bead")
	if err := f.tracker.SetNote(context.Background(), application.PosternAppliedKey("direct:done-1"), "applied comment mw-e.2 txid direct:done-1"); err != nil {
		t.Fatal(err)
	}

	report, printed := f.run(seq)

	if len(report.Postern) != 0 || strings.Contains(printed, "new postern message") {
		t.Fatalf("expected the applied message not to end the wait, got %+v:\n%s", report.Postern, printed)
	}

	seq = f.addMessage("direct:fresh-1", "", "a new word")
	report, printed = f.run(seq)
	if len(report.Postern) != 1 || report.Postern[0].Txid != "direct:fresh-1" || !strings.Contains(printed, "1 unread") {
		t.Fatalf("expected only the new message counted, got %+v:\n%s", report.Postern, printed)
	}
}
