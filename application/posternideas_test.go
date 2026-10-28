package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// flakyTracker is the fake tracker whose reads of the to-dos, and of named
// beads, fail while the matching field is set.
type flakyTracker struct {
	*apptest.FakeTracker
	readyErr, showErr error
}

func (f *flakyTracker) ReadyWithLabel(ctx context.Context, label string) ([]application.StoryDetail, error) {
	if f.readyErr != nil {
		return nil, f.readyErr
	}
	return f.FakeTracker.ReadyWithLabel(ctx, label)
}

func (f *flakyTracker) ShowBeads(ctx context.Context, ids []string) ([]application.StoryDetail, error) {
	if f.showErr != nil {
		return nil, f.showErr
	}
	return f.FakeTracker.ShowBeads(ctx, ids)
}

// ideasFlakyFixture is the Ideas-only inbox over a tracker that holds one open
// to-do, mw-lst.1 'Write the lesson plan', on the list 'Discipleship Institute'.
func ideasFlakyFixture(t *testing.T) (application.PosternInbox, *flakyTracker, *memoryIdeasFiles) {
	t.Helper()
	f, inbox, files := ideasTodoFixture(t)
	f.tracker.AddEpic("mw-lst", domain.Path{})
	f.tracker.DescribeEpic("mw-lst", "Discipleship Institute", apptest.StatusOpen, application.DefaultPriority)
	f.tracker.AddStory("mw-lst", domain.Story{ID: "mw-lst.1", Title: "Write the lesson plan"})
	mustDo(t, f.tracker.SetLabels("mw-lst.1", application.LabelHitl, application.LabelHitlTodo))
	flaky := &flakyTracker{FakeTracker: f.tracker}
	inbox.Tracker = flaky
	return inbox, flaky, files
}

// A tracker that cannot list the to-dos does not decide the message: nothing is
// kept, and the error names the read; on the next pass, with beads answering, a
// message sharing a word with an open to-do's title is a to-do and not kept.
func TestIdeasOnlyWeighsAgainWhenTheToDosCannotBeRead(t *testing.T) {
	inbox, tracker, files := ideasFlakyFixture(t)
	m := application.PosternInboxMessage{Txid: "tx-lesson", Class: "message", Thread: application.PosternGeneralThread, Text: "remind me about the lesson plan"}

	tracker.readyErr = errors.New("circuit breaker tripped")
	line, kept, _, err := inbox.HoldIdea(context.Background(), m)
	if err == nil || !strings.Contains(err.Error(), "reading the to-dos") || !strings.Contains(err.Error(), "circuit breaker tripped") {
		t.Fatalf("expected an error naming the read of the to-dos, got %v", err)
	}
	if kept || line != "" || len(files.held) != 0 {
		t.Fatalf("expected nothing kept on a failed read, got line %q kept %v held %+v", line, kept, files.held)
	}

	tracker.readyErr = nil
	if _, kept, _, err = inbox.HoldIdea(context.Background(), m); err != nil || kept || len(files.held) != 0 {
		t.Fatalf("expected the to-do-shaped message not kept once beads answer, got kept %v held %+v err %v", kept, files.held, err)
	}
}

// The same for the lists of the to-dos: the to-do cards are read but the lists
// they sit on are not.
func TestIdeasOnlyWeighsAgainWhenTheListsCannotBeRead(t *testing.T) {
	inbox, tracker, files := ideasFlakyFixture(t)
	m := application.PosternInboxMessage{Txid: "tx-list", Class: "message", Thread: application.PosternGeneralThread, Text: "notes for my discipleship institute"}

	tracker.showErr = errors.New("load too high")
	_, kept, _, err := inbox.HoldIdea(context.Background(), m)
	if err == nil || !strings.Contains(err.Error(), "reading the lists of the to-dos") || !strings.Contains(err.Error(), "load too high") {
		t.Fatalf("expected an error naming the read of the lists, got %v", err)
	}
	if kept || len(files.held) != 0 {
		t.Fatalf("expected nothing kept on a failed read, got kept %v held %+v", kept, files.held)
	}

	tracker.showErr = nil
	if _, kept, _, err = inbox.HoldIdea(context.Background(), m); err != nil || kept || len(files.held) != 0 {
		t.Fatalf("expected the message about the list not kept once beads answer, got kept %v held %+v err %v", kept, files.held, err)
	}
}

// The same for a message that names a bead: the read of that bead fails.
func TestIdeasOnlyWeighsAgainWhenANamedBeadCannotBeRead(t *testing.T) {
	inbox, tracker, files := ideasFlakyFixture(t)
	m := application.PosternInboxMessage{Txid: "tx-thread", Class: "message", Thread: "mw-lst.1", ThreadIsBead: true, Text: "done, thanks"}

	tracker.showErr = errors.New("circuit breaker tripped")
	_, kept, _, err := inbox.HoldIdea(context.Background(), m)
	if err == nil || !strings.Contains(err.Error(), "reading mw-lst.1") || !strings.Contains(err.Error(), "circuit breaker tripped") {
		t.Fatalf("expected an error naming the read of mw-lst.1, got %v", err)
	}
	if kept || len(files.held) != 0 {
		t.Fatalf("expected nothing kept on a failed read, got kept %v held %+v", kept, files.held)
	}

	tracker.showErr = nil
	if _, kept, _, err = inbox.HoldIdea(context.Background(), m); err != nil || kept || len(files.held) != 0 {
		t.Fatalf("expected the reply on a to-do not kept once beads answer, got kept %v held %+v err %v", kept, files.held, err)
	}
}

// A message that says 'to do' itself, or begins 'TODO:', needs no read, so a
// failing tracker does not hold it back.
func TestIdeasOnlyDoesNotReadWhenTheWordsSayToDo(t *testing.T) {
	inbox, tracker, files := ideasFlakyFixture(t)
	tracker.readyErr, tracker.showErr = errors.New("down"), errors.New("down")
	for _, text := range []string{"my to do for Friday", "TODO: an app"} {
		m := application.PosternInboxMessage{Txid: "tx-1", Class: "message", Thread: application.PosternGeneralThread, Text: text}
		if _, kept, _, err := inbox.HoldIdea(context.Background(), m); err != nil || kept || len(files.held) != 0 {
			t.Fatalf("%q: expected no error and not kept, got kept %v held %+v err %v", text, kept, files.held, err)
		}
	}
}
