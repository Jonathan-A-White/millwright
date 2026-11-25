package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1734: a story queued for batch landing whose path cannot be read was
// left out of every batch with a note in the report, and stayed queued for ever:
// the note is read by nobody, and a batch that lands nothing prints no more. It is
// handed back, its claim given and its run state cleared, with a comment saying
// why, and the Mayor is mailed its name, as a story the batches set aside again
// and again is.

// anUnreadablePathBatch is a batch rig of millwright whose only queued story,
// mw-e.1, has a model no harness runs on, so that its path cannot be read.
func anUnreadablePathBatch(t *testing.T, own domain.Path) (application.BatchLand, *apptest.FakeTracker, *apptest.FakeMailbox) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "The story mw-e.1", Overrides: own})
	mustDo(t, tracker.ClaimStory(ctx, "mw-e.1"))
	queue(t, tracker, "mw-e.1")
	mailbox := apptest.NewFakeMailbox()
	land := application.BatchLand{
		Next: application.Next{
			Tracker: tracker, Vault: newFakeVault(), Worktrees: &fetchOnly{}, Landing: dryLanding{},
			Checks: failingChecks{t}, Slot: noSlot{t}, Mailbox: mailbox, Host: "vps", Seat: "builder",
			Rigs: map[string]string{"millwright": "/rigs/millwright", "lampas": "/rigs/lampas"},
			Out:  &bytes.Buffer{},
		},
		Records: noRecords{t},
	}
	return land, tracker, mailbox
}

func TestAQueuedStoryWithAnUnreadablePathIsHandedBackOrMailed(t *testing.T) {
	ctx := context.Background()
	land, tracker, mailbox := anUnreadablePathBatch(t, domain.Path{Model: "no-such-model"})
	if _, err := tracker.ShowStory(ctx, "mw-e.1"); err != nil {
		t.Fatal(err)
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-e.1"); func() bool { _, err := detail.Path(); return err == nil }() {
		t.Fatal("the test does not drive what it names: the story's path can be read")
	}

	report, err := land.Run(ctx, "millwright")
	if err != nil {
		t.Fatalf("landing: %v", err)
	}

	if got := tracker.State("mw-e.1", application.RunState); got == application.RunQueued {
		t.Errorf("expected mw-e.1 taken off the batch queue, got run=%q", got)
	}
	detail, _ := tracker.ShowStory(ctx, "mw-e.1")
	if detail.Assignee != "" || detail.Status != application.StatusOpen {
		t.Errorf("expected the claim given back, the story open, got status %q held by %q", detail.Status, detail.Assignee)
	}
	comments := strings.Join(tracker.Comments("mw-e.1"), "\n---\n")
	for _, want := range []string{"path cannot be read", "model"} {
		if !strings.Contains(comments, want) {
			t.Errorf("expected a comment on mw-e.1 naming %q, got:\n%s", want, comments)
		}
	}
	if joined := strings.Join(report.Notes, "\n"); !strings.Contains(joined, "mw-e.1 was handed back") {
		t.Errorf("expected a note that mw-e.1 was handed back, got %q", report.Notes)
	}
	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Subject+inbox[0].Body, "mw-e.1") {
		t.Errorf("expected one mail to the Mayor naming mw-e.1, got %+v", inbox)
	}
}

func TestAnotherRigsStoryWithAnUnreadablePathIsNotTouchedByThisRigsBatch(t *testing.T) {
	ctx := context.Background()
	land, tracker, mailbox := anUnreadablePathBatch(t, domain.Path{Rig: "lampas", Model: "no-such-model"})

	report, err := land.Run(ctx, "millwright")
	if err != nil {
		t.Fatalf("landing: %v", err)
	}
	if got := tracker.State("mw-e.1", application.RunState); got != application.RunQueued {
		t.Errorf("expected the story of lampas left queued by millwright's batch, got run=%q", got)
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("expected no mail, got %+v", inbox)
	}
	if joined := strings.Join(report.Notes, "\n"); strings.Contains(joined, "mw-e.1") {
		t.Errorf("expected no note on another rig's story, got %q", report.Notes)
	}
}

func TestADryRunLeavesAQueuedStoryWithAnUnreadablePathAsItWasAndSaysSo(t *testing.T) {
	ctx := context.Background()
	land, tracker, mailbox := anUnreadablePathBatch(t, domain.Path{Model: "no-such-model"})
	land.DryRun = true

	report, err := land.Run(ctx, "millwright")
	if err != nil {
		t.Fatalf("landing: %v", err)
	}
	if got := tracker.State("mw-e.1", application.RunState); got != application.RunQueued {
		t.Errorf("expected a dry run to change nothing, got run=%q", got)
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("expected a dry run to mail nothing, got %+v", inbox)
	}
	if joined := strings.Join(report.Notes, "\n"); !strings.Contains(joined, "mw-e.1") || !strings.Contains(joined, "path cannot be read") {
		t.Errorf("expected a note naming mw-e.1 and its path, got %q", report.Notes)
	}
}
