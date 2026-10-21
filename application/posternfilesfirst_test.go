package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// Whichever route applies a Governor message with files first saves them: the
// Mayor's mw postern inbox, read before any Apply pass, writes both files of a
// general-thread or named-channel message under their txid names, records them
// in the applied note, and the Apply pass after it neither applies the message
// again nor loses the files (mw-gq6.686).
func TestInboxReadingFirstSavesTheFilesOfAGeneralOrNamedMessageAndApplyKeepsThem(t *testing.T) {
	for _, tc := range []struct {
		name   string
		thread map[string]any
	}{
		{"general thread", nil},
		{"named channel", map[string]any{"topic": "screens"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newManyFiles(t)
			one := f.add(t, "image/png", pngBytes)
			two := f.add(t, "image/jpeg", jpegBytes)
			body := map[string]any{"text": "two shots", "attachments": []any{one, two}}
			if tc.thread != nil {
				body["thread"] = tc.thread
			}
			f.post(t, "first-txid", body)
			out := &bytes.Buffer{}

			if _, err := f.inbox(out).Run(context.Background()); err != nil {
				t.Fatalf("reading: %v", err)
			}
			p1 := f.assertWritten(t, "first-txid-1.png", pngBytes)
			p2 := f.assertWritten(t, "first-txid-2.jpg", jpegBytes)
			for _, p := range []string{p1, p2} {
				if !strings.Contains(out.String(), p) {
					t.Errorf("expected %s printed, got:\n%s", p, out.String())
				}
			}
			noted, err := f.tracker.Note(context.Background(), application.PosternAppliedKey("first-txid"))
			if err != nil {
				t.Fatalf("reading the applied note: %v", err)
			}
			if !strings.Contains(noted, p1) || !strings.Contains(noted, p2) {
				t.Errorf("expected the applied note to name both files, got %q", noted)
			}

			inbox := f.inbox(&bytes.Buffer{})
			mailbox := apptest.NewFakeMailbox()
			inbox.Mailbox = mailbox
			lines, err := inbox.Apply(context.Background())
			if err != nil {
				t.Fatalf("applying: %v", err)
			}
			if len(lines) != 0 {
				t.Errorf("expected the Apply pass to apply nothing again, got %v", lines)
			}
			f.assertWritten(t, "first-txid-1.png", pngBytes)
			f.assertWritten(t, "first-txid-2.jpg", jpegBytes)
			if again, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("first-txid")); again != noted {
				t.Errorf("expected the applied note unchanged by the Apply pass, got %q", again)
			}
		})
	}
}

// The other order: the Apply pass first saves the files, and the Mayor's read
// after it shows the one applied line with his whole text and the paths, and
// saves nothing again (mw-gq6.686).
func TestApplyFirstSavesTheFilesOfAGeneralMessageAndInboxShowsThem(t *testing.T) {
	f := newManyFiles(t)
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	f.post(t, "apply-first", map[string]any{"text": "two shots", "attachments": []any{one, two}})
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Mailbox = apptest.NewFakeMailbox()
	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	p1 := f.assertWritten(t, "apply-first-1.png", pngBytes)
	p2 := f.assertWritten(t, "apply-first-2.jpg", jpegBytes)

	out := &bytes.Buffer{}
	if _, err := f.inbox(out).Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	for _, want := range []string{"already applied earlier: ", p1, p2, "two shots"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q printed, got:\n%s", want, out.String())
		}
	}
}
