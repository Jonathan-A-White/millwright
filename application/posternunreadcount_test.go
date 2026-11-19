package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1500: the count the pane notifier types ("New postern message for
// mayor (N unread)") is what mw postern inbox lists as a message: a message a
// pass already applied is one line "already applied earlier: ...", never a
// message to read, so it is not counted.

// headers counts the messages a read prints: each has a header naming its
// channel; an applied message is one "already applied earlier" line.
func headers(out string) int { return strings.Count(out, "channel ") }

func TestUnreadCountLeavesOutWhatAPassAlreadyApplied(t *testing.T) {
	f := newPartsFixture(t)
	f.addPlain("direct:applied-1", "already dealt with one")
	f.addPlain("direct:applied-2", "already dealt with two")
	for _, txid := range []string{"direct:applied-1", "direct:applied-2"} {
		if err := f.tracker.SetNote(context.Background(), application.PosternAppliedKey(txid), "applied answer mw-e.3 txid "+txid); err != nil {
			t.Fatal(err)
		}
	}

	count, err := f.reader(&bytes.Buffer{}).UnreadCount(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("expected 0 unread with only applied messages, got %d, %v", count, err)
	}
	out := &bytes.Buffer{}
	if _, err := f.reader(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if got := headers(out.String()); got != 0 {
		t.Errorf("expected the listing to show no message, got %d:\n%s", got, out.String())
	}

	// One new message of his: both say 1.
	f.addPlain("direct:new", "something new")
	count, err = f.reader(&bytes.Buffer{}).UnreadCount(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("expected 1 unread with one new message, got %d, %v", count, err)
	}
	out.Reset()
	if _, err := f.reader(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if got := headers(out.String()); got != 1 || !strings.Contains(out.String(), "something new") {
		t.Errorf("expected the listing to show the one new message, got:\n%s", out.String())
	}
}

// An applied comment's words are printed by the read, the Mayor's only sight
// of them, so the count keeps it.
func TestUnreadCountKeepsAnAppliedCommentWhoseWordsTheReadPrints(t *testing.T) {
	f := newPartsFixture(t)
	f.addPlain("direct:words", "words for the bead")
	if err := f.tracker.SetNote(context.Background(), application.PosternAppliedKey("direct:words"), "applied comment mw-e.2 txid direct:words"); err != nil {
		t.Fatal(err)
	}
	count, err := f.reader(&bytes.Buffer{}).UnreadCount(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("expected the comment counted, got %d, %v", count, err)
	}
	out := &bytes.Buffer{}
	if _, err := f.reader(out).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "words for the bead") {
		t.Errorf("expected the words printed, got:\n%s", out.String())
	}
}
