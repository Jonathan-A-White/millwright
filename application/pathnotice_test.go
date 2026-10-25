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

// everyRigExpectsFiles says every rig's file in the vault has files_expected = true.
type everyRigExpectsFiles struct{}

func (everyRigExpectsFiles) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{FilesExpected: true}, nil
}

// anUnheldStory holds a story of the factory's epic and lets it go again, as
// the Mayor does when he un-holds one without mw release (mw-gq6.599).
// The story names the files given, or none.
func anUnheldStory(t *testing.T, tracker *apptest.FakeTracker, id, files string) {
	t.Helper()
	story := domain.Story{ID: id, Title: "A story the Mayor un-held"}
	if files != "" {
		if err := story.Overrides.Set("files", files); err != nil {
			t.Fatalf("pathing %s: %v", id, err)
		}
	}
	tracker.AddStory("mw-gq6", story)
	if err := tracker.HoldStory(context.Background(), id); err != nil {
		t.Fatalf("holding %s: %v", id, err)
	}
	if err := tracker.SetStatus(id, apptest.StatusOpen); err != nil {
		t.Fatalf("un-holding %s: %v", id, err)
	}
}

func TestUnholdingAStoryWithNoFilesKeyTellsTheMayorTheCommandOnce(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Mailbox, dispatch.Rules = mailbox, everyRigExpectsFiles{}
	anUnheldStory(t, tracker, "mw-gq6.1", "")

	for tick := 1; tick <= 3; tick++ {
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if len(report.Started) != 0 {
			t.Fatalf("tick %d: started %v, want nothing", tick, startedIDs(report))
		}
	}
	if len(worktrees.added) != 0 || len(runner.Started()) != 0 {
		t.Errorf("a worktree or session was made: %v %v", worktrees.added, runner.Started())
	}
	if got := mailbox.Writes(); got != 1 {
		t.Fatalf("the Mayor should be mailed once over three ticks, got %d", got)
	}
	inbox, _ := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "mw-gq6.1") || !strings.Contains(inbox[0].Body, "mw path set mw-gq6.1 files=<csv>") {
		t.Errorf("the mail should name the story and the exact command, got %+v", inbox)
	}
	if comments := tracker.Comments("mw-gq6.1"); len(comments) != 1 || !strings.Contains(comments[0], "mw path set mw-gq6.1 files=<csv>") {
		t.Errorf("one comment should say it, got %q", comments)
	}
}

func TestAStoryWithEveryKeySetRaisesNoPathKeyNotice(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Mailbox, dispatch.Rules = mailbox, everyRigExpectsFiles{}
	anUnheldStory(t, tracker, "mw-gq6.1", "application/x.go")
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 {
		t.Errorf("started %v, want the story", ids)
	}
	if mailbox.Writes() != 0 || len(tracker.Comments("mw-gq6.1")) != 0 {
		t.Errorf("a story with every key set should raise nothing: %d mails, %q", mailbox.Writes(), tracker.Comments("mw-gq6.1"))
	}
}

func TestADryRunDispatchTellsNoOneOfAMissingFilesKey(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Mailbox, dispatch.Rules, dispatch.DryRun = mailbox, everyRigExpectsFiles{}, true
	anUnheldStory(t, tracker, "mw-gq6.1", "")
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if mailbox.Writes() != 0 || len(tracker.Comments("mw-gq6.1")) != 0 {
		t.Errorf("a dry run writes nothing: %d mails, %q", mailbox.Writes(), tracker.Comments("mw-gq6.1"))
	}
}

func TestStatusDampersShowAnOpenStoryWithNoFilesKeyFromTheMomentItIsOpen(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.70", "Just un-held", "vps")
	keyed := domain.Story{ID: "mw-gq6.71", Title: "Keyed"}
	_ = keyed.Overrides.Set("host", "vps")
	_ = keyed.Overrides.Set("files", "a.go")
	tracker.AddStory("mw-gq6", keyed)
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", Rules: everyRigExpectsFiles{},
		Now: func() time.Time { return statusNow },
	}
	report, err := status.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	t.Logf("\n%s", printed)
	var line string
	for _, alarm := range report.Dampers.Alarms {
		if alarm.Principle == application.DamperNeedsFilesKey {
			line = alarm.Line()
		}
	}
	if !strings.Contains(line, "mw-gq6.70") || strings.Contains(line, "mw-gq6.71") || !strings.Contains(line, "mw path set <id> files=<csv>") {
		t.Errorf("the DAMPERS should name the story with no files key, and only it, with the command: %q", line)
	}
	if !strings.Contains(printed, application.DampersHeading) {
		t.Errorf("expected the DAMPERS section:\n%s", printed)
	}
}

func TestStatusDampersAreQuietWhenEveryStoryHasItsFilesKey(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	keyed := domain.Story{ID: "mw-gq6.71", Title: "Keyed"}
	_ = keyed.Overrides.Set("host", "vps")
	_ = keyed.Overrides.Set("files", "a.go")
	tracker.AddStory("mw-gq6", keyed)
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", Rules: everyRigExpectsFiles{},
		Now: func() time.Time { return statusNow },
	}
	report, err := status.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	for _, alarm := range report.Dampers.Alarms {
		if alarm.Principle == application.DamperNeedsFilesKey {
			t.Errorf("expected no files-key alarm, got %q", alarm.Line())
		}
	}
}
