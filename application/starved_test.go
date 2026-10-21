package application_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.580, sweep: every reason mw dispatch passes a ready story over for is
// a hold the starved-rig alarm can name. A new gate, or a new Passed in
// dispatch.go with no gate, fails here until it is mapped.
func TestEveryReasonDispatchPassesAStoryOverIsMappedToAHoldName(t *testing.T) {
	for _, gate := range application.DispatchGates {
		if gate.Name == "paused" {
			continue // a paused host passes no story: it does nothing at all
		}
		if application.StarvedHolds[gate.Name] == "" {
			t.Errorf("the gate %q has no hold name in application.StarvedHolds", gate.Name)
		}
	}
	if application.StarvedHolds[application.PourRefusedGate] == "" {
		t.Errorf("the pour refusal has no hold name in application.StarvedHolds")
	}
	known := map[string]bool{application.PourRefusedGate: true, application.ClaimLostGate: true, application.LeftoverRefusedGate: true}
	for _, gate := range application.DispatchGates {
		known[gate.Name] = true
	}
	for gate := range application.StarvedHolds {
		if !known[gate] {
			t.Errorf("application.StarvedHolds names %q, which is no dispatch gate", gate)
		}
	}

	source, err := os.ReadFile("dispatch.go")
	if err != nil {
		t.Fatal(err)
	}
	passes := strings.Count(string(source), "Passed{StoryID:")
	var named int
	for _, found := range regexp.MustCompile(`Passed\{StoryID: id, Gate: (?:"([^"]+)"|(\w+)),`).FindAllStringSubmatch(string(source), -1) {
		named++
		if found[1] != "" && !known[found[1]] {
			t.Errorf("dispatch.go passes a story over for the gate %q, which is not in DispatchGates", found[1])
		}
	}
	if named != passes {
		t.Errorf("dispatch.go builds %d Passed values and names a gate in %d: every one carries its gate", passes, named)
	}
}

// aSmokeHeldRig is a dispatch over a rig whose grist smoke failed, holding two
// of its stories.
func aSmokeHeldRig(t *testing.T) (application.Dispatch, *apptest.FakeTracker, application.GristSmokeBook) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Another story"})
	book := application.GristSmokeBook{Notes: tracker, Host: "laptop"}
	dispatch.SmokeHolds, dispatch.Memory = book, tracker
	failed := application.GristSmokeReport{App: "cairn", Examples: 1, Failures: []string{"cairn/sweep/one: the mill refused it"}}
	if err := book.Record(context.Background(), "millwright", failed); err != nil {
		t.Fatalf("recording the smoke: %v", err)
	}
	return dispatch, tracker, book
}

func TestDispatchKeepsSinceWhenARigWithReadyStoriesAndNoneRunningWasFirstHeld(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, book := aSmokeHeldRig(t)
	first := time.Date(2026, 10, 17, 18, 38, 0, 0, time.UTC)
	clock := first
	dispatch.Now = func() time.Time { return clock }

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	key := application.StarvedKey("vps")
	kept, _ := tracker.Note(ctx, key)
	records := application.ParseStarved(kept)
	if len(records) != 1 || records[0].Rig != "millwright" || !records[0].Since.Equal(first) ||
		records[0].Hold != "smoke hold" || !strings.Contains(records[0].Reason, "grist smoke of cairn failed") {
		t.Fatalf("expected the note %s to name millwright held by the smoke hold since %s, got %q", key, first, kept)
	}

	// Held again three hours on, it was held since the first time, and the note
	// is not written again.
	clock = first.Add(3 * time.Hour)
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching again: %v", err)
	}
	if again, _ := tracker.Note(ctx, key); again != kept {
		t.Errorf("expected the note to keep %q, got %q", kept, again)
	}
	if writes := tracker.NoteWrites(key); writes != 1 {
		t.Errorf("expected the note written once, got %d writes", writes)
	}

	// The smoke passes: a story starts, and the rig is no longer held.
	if err := book.Record(ctx, "", application.GristSmokeReport{App: "cairn", Examples: 1}); err != nil {
		t.Fatal(err)
	}
	if report, err := dispatch.Run(ctx); err != nil || len(report.Started) != 1 {
		t.Fatalf("expected a story started once the smoke passed, got %+v %v", report.Started, err)
	}
	if after, _ := tracker.Note(ctx, key); after != "" {
		t.Errorf("expected the note cleared once a story started, got %q", after)
	}
}

func TestADryRunKeepsNoNoteOfAStarvedRig(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _ := aSmokeHeldRig(t)
	dispatch.DryRun = true
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if writes := tracker.NoteWrites(application.StarvedKey("vps")); writes != 0 {
		t.Errorf("expected a dry run to write no note, got %d writes", writes)
	}
}

func TestARigRunningAStoryIsNotKeptAsStarved(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Memory, dispatch.Cap = tracker, 1
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Running"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Waits for the cap"})
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if kept, _ := tracker.Note(ctx, application.StarvedKey("vps")); kept != "" {
		t.Errorf("expected no starved note for a rig that started a story, got %q", kept)
	}
}

// starvedFixture is a tracker holding the two stories of rig millwright ready,
// and the note a dispatch left of the rig held by its smoke since ago.
func starvedFixture(t *testing.T, ago time.Duration) *apptest.FakeTracker {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.1", "One", "vps")
	storyOn(t, tracker, "mw-gq6.2", "Two", "vps")
	note := application.StarvedRig{
		Rig: "millwright", Since: statusNow.Add(-ago), Hold: "smoke hold",
		Reason: "the grist smoke of cairn failed (cairn/sweep/one)",
	}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("vps"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	return tracker
}

func starvedAlarms(t *testing.T, tracker *apptest.FakeTracker) []application.DamperAlarm {
	t.Helper()
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker}
	alarms, err := survey.Starved(context.Background(), statusNow)
	if err != nil {
		t.Fatalf("reading the starved rigs: %v", err)
	}
	return alarms
}

func TestARigStarvedOverAnHourIsAnAlarmNamingTheRigTheCountAndTheHold(t *testing.T) {
	alarms := starvedAlarms(t, starvedFixture(t, 4*time.Hour))
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm, got %+v", alarms)
	}
	line := alarms[0].Line()
	for _, want := range []string{"starved", "millwright", "2 ready", "none running for 4h00m", "smoke hold", "the grist smoke of cairn failed", "mw-gq6.1"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q in the line, got %q", want, line)
		}
	}
	if strings.Contains(line, "\n") {
		t.Errorf("expected one line, got %q", line)
	}
}

func TestARigHeldForUnderAnHourIsNoAlarm(t *testing.T) {
	if alarms := starvedAlarms(t, starvedFixture(t, 59*time.Minute)); len(alarms) != 0 {
		t.Errorf("expected no alarm inside the hour, got %+v", alarms)
	}
}

func TestAStarvedRigClearsWhenAStoryOfItIsRunning(t *testing.T) {
	tracker := starvedFixture(t, 4*time.Hour)
	if err := tracker.ClaimStory(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if alarms := starvedAlarms(t, tracker); len(alarms) != 0 {
		t.Errorf("expected the alarm cleared by a running story, got %+v", alarms)
	}
}

func TestAStarvedRigWithNothingReadyIsNoAlarm(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	note := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: "smoke hold", Reason: "x"}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("vps"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	if alarms := starvedAlarms(t, tracker); len(alarms) != 0 {
		t.Errorf("expected no alarm with nothing ready, got %+v", alarms)
	}
}

func TestStatusDampersShowTheStarvedRigLine(t *testing.T) {
	tracker := starvedFixture(t, 4*time.Hour)
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Now:     func() time.Time { return statusNow },
		Dampers: application.DamperSurvey{Tracker: tracker, Notes: tracker},
	}
	report, err := status.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	want := starvedAlarms(t, tracker)[0].Line()
	if !strings.Contains(printed, application.DampersHeading) || !strings.Contains(printed, "millwright: 2 ready, none running for 4h00m") ||
		!strings.Contains(printed, "smoke hold") {
		t.Errorf("expected the DAMPERS section to carry the starved line %q, got:\n%s", want, printed)
	}
}

// aStarvedRigCheck is the check over a tracker that changes under it, with a
// mailbox, run by a doctor.
func aStarvedRigCheck(tracker *apptest.FakeTracker, mailbox *apptest.FakeMailbox) *application.StarvedRigCheck {
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker}
	return &application.StarvedRigCheck{
		Host:   "vps",
		Alarms: func(ctx context.Context) ([]application.DamperAlarm, error) { return survey.Starved(ctx, statusNow) },
		Mail:   func(ctx context.Context, m application.NewMessage) error { _, err := mailbox.Send(ctx, m); return err },
	}
}

func TestMwDoctorAlarmsAStarvedRigAndMailsTheMayorOncePerEpisode(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	state, log := apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}
	now := doctorNow
	doctor := aDoctor(state, log, &now, aStarvedRigCheck(tracker, mailbox))

	report, err := doctor.Run(ctx, "", false)
	if application.ExitStatus(err) != 0 {
		t.Fatalf("expected the first tick to cure by mailing, got %v", err)
	}
	if got := report.Results[0].Reason; !strings.Contains(got, "millwright: 2 ready, none running for 4h00m") || !strings.Contains(got, "smoke hold") {
		t.Errorf("expected the alarm to name the rig, the count and the hold, got %q", got)
	}
	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Subject, "millwright") ||
		!strings.Contains(inbox[0].Body, "2 ready, none running for 4h00m") || !strings.Contains(inbox[0].Body, "the grist smoke of cairn failed") {
		t.Fatalf("expected one mail to the Mayor naming the rig, the count and the hold, got %+v", inbox)
	}

	// Ticks later it is the same episode: no more mail.
	for range 3 {
		now = now.Add(5 * time.Minute)
		if _, err := doctor.Run(ctx, "", false); err == nil {
			t.Errorf("expected the starved rig to stay a fault while it lasts")
		}
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 1 {
		t.Errorf("expected the Mayor mailed once for the episode, got %d mails", len(inbox))
	}

	// A story of the rig starts: the alarm clears.
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute)
	report, err = doctor.Run(ctx, "", false)
	if err != nil || report.Results[0].Verdict != "ok" {
		t.Fatalf("expected the check ok once a story of the rig runs, got %+v %v", report.Results, err)
	}
}

func TestMwDoctorMailsAgainForANewStarvedEpisode(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	state, log := apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}
	now := doctorNow
	doctor := aDoctor(state, log, &now, aStarvedRigCheck(tracker, mailbox))
	if _, err := doctor.Run(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := doctor.Run(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := doctor.Run(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Errorf("expected a mail for each episode, got %d", len(inbox))
	}
}

func TestMwDoctorOnAHealthyFactoryIsQuietAboutStarvedRigs(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	mailbox := apptest.NewFakeMailbox()
	check := aStarvedRigCheck(tracker, mailbox)
	if verdict, reason := check.Probe(ctx); verdict != application.DoctorOK || reason != "" {
		t.Errorf("expected ok, got %v %q", verdict, reason)
	}
}
