package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const (
	acceptanceWhenClaimed = "1. It works.\n2. AC 4 is met.\n3. AC 5 is met."
	acceptanceNow         = "1. It works.\n2. AC 4 is met by the Mayor's new wording.\n3. AC 5 is dropped."
)

// aHandedBackStoryWhoseAcceptanceChanged is the shape of mw-82lb97i.27: the
// Builder handed it back asking for a decision, its claim recorded the old
// acceptance, and the Mayor has since re-scoped it (mw-gq6.1166).
func aHandedBackStoryWhoseAcceptanceChanged(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	aHandedBackStory(t, tracker, id)
	if err := tracker.SetAcceptance(id, acceptanceWhenClaimed); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AcceptanceAtClaimField: application.AcceptanceAtClaim(acceptanceWhenClaimed)}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetAcceptance(id, acceptanceNow); err != nil {
		t.Fatal(err)
	}
}

func TestADispatchRePathsAHandedBackStoryWhoseAcceptanceChangedAndTakesIt(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aHandedBackStoryWhoseAcceptanceChanged(t, tracker, "mw-gq6.1")

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	comments := tracker.Comments("mw-gq6.1")
	if len(comments) != 2 || !strings.HasPrefix(comments[1], application.RePathedMark) {
		t.Fatalf("expected a RE-PATHED: comment after the hand-back, got %q", comments)
	}
	for _, want := range []string{"2. AC 4 is met by the Mayor's new wording.", "3. AC 5 is dropped.", "acceptance"} {
		if !strings.Contains(comments[1], want) {
			t.Errorf("the RE-PATHED: comment lacks %q: %q", want, comments[1])
		}
	}
	if strings.Contains(comments[1], "1. It works.") {
		t.Errorf("the comment quotes a line that did not change: %q", comments[1])
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Errorf("started %v, want the re-pathed story taken by the same tick", ids)
	}
}

func TestADispatchLeavesAHandedBackStoryWhoseAcceptanceIsTheSameAsItWas(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aHandedBackStoryWhoseAcceptanceChanged(t, tracker, "mw-gq6.1")
	if err := tracker.SetAcceptance("mw-gq6.1", "1. It works.\n\n2. AC 4 is met.\n3.  AC 5 is met."); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(tracker.Comments("mw-gq6.1")) != 1 {
		t.Errorf("only re-spacing changed: started %v, comments %q", startedIDs(report), tracker.Comments("mw-gq6.1"))
	}
}

func TestADispatchLeavesAHandedBackStoryWithNoClaimRecordAlone(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aHandedBackStory(t, tracker, "mw-gq6.1")
	if err := tracker.SetAcceptance("mw-gq6.1", acceptanceNow); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(tracker.Comments("mw-gq6.1")) != 1 {
		t.Errorf("a story claimed before the record was kept is not compared: started %v, comments %q", startedIDs(report), tracker.Comments("mw-gq6.1"))
	}
}

func TestADryRunSaysItWouldRePathAChangedHandedBackStoryAndWritesNothing(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.DryRun = true
	aHandedBackStoryWhoseAcceptanceChanged(t, tracker, "mw-gq6.1")
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(tracker.Comments("mw-gq6.1")) != 1 {
		t.Errorf("a dry run writes nothing: started %v, comments %q", startedIDs(report), tracker.Comments("mw-gq6.1"))
	}
	if len(report.Passed) != 1 || !strings.Contains(report.Passed[0].Why, "acceptance changed") {
		t.Errorf("the pass should say the acceptance changed and a real tick re-paths it, got %+v", report.Passed)
	}
}

// aRefusedCheck is the doctor check whose refused-open alarms are read from
// work, as the survey reads them from the tracker (mw-gq6.1166, AC 2).
func aRefusedCheck(work *application.WorkInHand, mailbox *apptest.FakeMailbox, tracker *apptest.FakeTracker) *application.StarvedRigCheck {
	return &application.StarvedRigCheck{
		Host:   "laptop",
		Alarms: func(context.Context) ([]application.DamperAlarm, error) { return nil, nil },
		Refused: func(context.Context) ([]application.DamperAlarm, error) {
			var alarms []application.DamperAlarm
			for _, o := range application.OpenRefusedStories(*work, stuckNow) {
				alarms = append(alarms, o.Alarm())
			}
			return alarms, nil
		},
		Mail:   func(ctx context.Context, m application.NewMessage) error { _, err := mailbox.Send(ctx, m); return err },
		Mailed: tracker,
	}
}

func TestARefusedOpenStoryOverHalfAnHourIsMailedToTheMayorOnceWithTheCommandToGoOn(t *testing.T) {
	ctx := context.Background()
	work := &application.WorkInHand{Ready: []application.StoryDetail{
		refusedOpen("mw-q.3", application.RunBlocked, 45*time.Minute),
		refusedOpen("mw-q.4", application.RunBlocked, 10*time.Minute),
	}}
	mailbox, tracker := apptest.NewFakeMailbox(), apptest.NewFakeTracker()
	check := aRefusedCheck(work, mailbox, tracker)

	for tick := 1; tick <= 3; tick++ {
		verdict, _ := check.Probe(ctx)
		if verdict != application.DoctorOK {
			t.Fatalf("tick %d: a refused-open story is the Mayor's to know, not a fault to page: %v", tick, verdict)
		}
	}
	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 {
		t.Fatalf("three ticks over one refused story: %d mails, want 1: %+v", len(inbox), inbox)
	}
	if !strings.HasPrefix(inbox[0].Subject, application.MailStarved+":") {
		t.Errorf("the mail is the Starved mail, got subject %q", inbox[0].Subject)
	}
	for _, want := range []string{"mw-q.3", "45m", "run:blocked", "mw retry mw-q.3"} {
		if !strings.Contains(inbox[0].Body, want) {
			t.Errorf("the mail lacks %q:\n%s", want, inbox[0].Body)
		}
	}
	if strings.Contains(inbox[0].Body, "mw-q.4") {
		t.Errorf("a story refused 10 minutes ago is not yet named:\n%s", inbox[0].Body)
	}

	// Another story turns 30 minutes old: only it is mailed, the first is not again.
	work.Ready = append(work.Ready, refusedOpen("mw-q.5", application.RunStopped, 31*time.Minute))
	check.Probe(ctx)
	check.Probe(ctx)
	inbox, _ = mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 2 {
		t.Fatalf("a second story: %d mails, want 2", len(inbox))
	}
	if strings.Contains(inbox[1].Body, "mw-q.3") || !strings.Contains(inbox[1].Body, "mw retry mw-q.5") {
		t.Errorf("the second mail should name only the new story:\n%s", inbox[1].Body)
	}

	// The first is retried and refused again later: that is news again.
	work.Ready = work.Ready[:0]
	check.Probe(ctx)
	work.Ready = append(work.Ready, refusedOpen("mw-q.3", application.RunBlocked, 50*time.Minute))
	check.Probe(ctx)
	if inbox, _ = mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 3 {
		t.Errorf("a story refused again after it moved on: %d mails, want 3", len(inbox))
	}
}

func TestARefusedOpenStoryIsNotMailedByADryRunOrWithNothingToKeepWhatWasMailed(t *testing.T) {
	ctx := context.Background()
	work := &application.WorkInHand{Ready: []application.StoryDetail{refusedOpen("mw-q.3", application.RunBlocked, 45*time.Minute)}}
	mailbox, tracker := apptest.NewFakeMailbox(), apptest.NewFakeTracker()
	check := aRefusedCheck(work, mailbox, tracker)
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, check)
	if _, err := doctor.Run(ctx, "", true); err != nil {
		t.Fatalf("the dry run: %v", err)
	}
	check.Mailed = nil
	check.Probe(ctx)
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("a dry run, or no record of what was mailed, mails nothing: %+v", inbox)
	}
}

// A close-out that refused a story and kept its claim leaves it in progress with
// run:blocked and no session: the same stuck state as one given back, named and
// mailed by the same rule (mw-gq6.1166, AC 3).
func TestARefusedClaimKeptInProgressIsNamedAndMailedLikeAnOpenRefusedStory(t *testing.T) {
	kept := refusedOpen("mw-q.8", application.RunBlocked, 40*time.Minute)
	kept.Status = "in_progress"
	kept.Assignee = "mw@laptop"
	work := application.WorkInHand{Running: []application.StoryDetail{kept}}
	alarms := openRefusedAlarms(work)
	if len(alarms) != 1 || !strings.Contains(alarms[0].Line(), "mw retry mw-q.8") || !strings.Contains(alarms[0].Line(), "40m") {
		t.Fatalf("the kept refused claim should raise one alarm with its age and command, got %+v", alarms)
	}
	young := kept
	young.Updated = stuckNow.Add(-10 * time.Minute)
	if got := openRefusedAlarms(application.WorkInHand{Running: []application.StoryDetail{young}}); len(got) != 0 {
		t.Errorf("a claim refused 10 minutes ago raised %+v", got)
	}
}
