package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mailBodyOf is the body of the one mail the Apply pass sent the Mayor.
func mailBodyOf(t *testing.T, mailbox *apptest.FakeMailbox) string {
	t.Helper()
	mail, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatalf("reading the Mayor's mail: %v", err)
	}
	if len(mail) != 1 {
		t.Fatalf("expected one mail, got %d", len(mail))
	}
	return mail[0].Body
}

// The Apply pass's 'Governor on <bead>' mail names the pictures his comment
// carried, so a Mayor reading the mail learns a screenshot came with it
// (mw-gq6.685).
func TestApplyMailNamesThePicturesACommentCarries(t *testing.T) {
	f := newManyFiles(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-many.2"})
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	f.post(t, "pics-txid", map[string]any{
		"thread": map[string]any{"bead": "mw-many.2"}, "text": "see both", "attachments": []any{one, two},
	})
	mailbox := apptest.NewFakeMailbox()
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Mailbox = mailbox

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	p1 := filepath.Join(f.dir, "pics-txid-1.png")
	p2 := filepath.Join(f.dir, "pics-txid-2.jpg")
	want := "see both [carries 2 pictures: " + p1 + ", " + p2 + "]"
	if body := mailBodyOf(t, mailbox); !strings.Contains(body, want) {
		t.Fatalf("expected the mail to hold %q, got:\n%s", want, body)
	}
}

// A comment with no files is mailed as it always was.
func TestApplyMailOfACommentWithNoFilesIsUnchanged(t *testing.T) {
	f := newApplyFixture(t)
	threaded, _ := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: "mw-e.2"}, Text: "looks fine to me"})
	f.message(t, releaseTapGovernorKey, "tx-plain", string(threaded))

	f.apply(t)

	body := mailBodyOf(t, f.mailbox)
	if strings.Contains(body, "carries") {
		t.Fatalf("expected no picture suffix on a text-only comment, got:\n%s", body)
	}
	if !strings.Contains(body, "on mw-e.2:\n\nlooks fine to me") {
		t.Fatalf("expected the comment's words, got:\n%s", body)
	}
}

// A fresh text-only comment of the Governor's that the Mayor's own read
// applies is printed whole, every line, and is not applied a second time by
// the read after it (mw-gq6.685).
func TestInboxPrintsAFreshTextOnlyCommentWholeAndAppliesItOnce(t *testing.T) {
	f := newApplyFixture(t)
	text := "Q1 = B, Fable xhigh.\nQ2 = A.\nThis needs tlc across the board."
	threaded, _ := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: "mw-e.2"}, Text: text})
	f.message(t, releaseTapGovernorKey, "tx-fresh", string(threaded))

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	got := f.out.String()
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("expected the line %q printed, got %q", line, got)
		}
	}
	if comments := f.tracker.Comments("mw-e.2"); len(comments) != 1 {
		t.Fatalf("expected the comment applied once, got %v", comments)
	}

	f.out.Reset()
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading again: %v", err)
	}
	if comments := f.tracker.Comments("mw-e.2"); len(comments) != 1 {
		t.Fatalf("expected the comment still applied once, got %v", comments)
	}
	if got := f.mailbox; got == nil {
		t.Fatal("no mailbox")
	}
	if subjects := f.subjects(t); len(subjects) != 1 {
		t.Fatalf("expected one mail after two reads, got %v", subjects)
	}
}
