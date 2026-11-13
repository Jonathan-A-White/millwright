package application_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
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
	known := map[string]bool{application.PourRefusedGate: true, application.ClaimLostGate: true, application.LeftoverRefusedGate: true, application.HeldAfterClaimGate: true}
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

// mw-gq6.710: a rig held only by a usage-limit hold note on its host (the one mw
// pause sets, or a real limit) is waiting on a time that is known, not starved;
// the same rig under another damper still is.
func TestARigHeldOnlyByAUsageLimitHoldNoteRaisesNoStarvedAlarm(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Another story"})
	dispatch.Memory = tracker
	first := time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return first }
	hold := first.Add(40*time.Hour).Format(time.RFC3339) + " " + string(application.ReasonUsageLimit) + " mw pause: the Governor's word"
	if err := tracker.SetNote(ctx, application.HarnessHoldKey("vps"), hold); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil || len(report.Started) != 0 || len(report.Passed) != 2 {
		t.Fatalf("expected both stories passed over for the hold, got %+v %v", report, err)
	}
	if kept, _ := tracker.Note(ctx, application.StarvedKey("vps")); kept != "" {
		t.Errorf("expected no starved note for a rig held only by the usage-limit hold, got %q", kept)
	}

	// Even a note some host kept before, for the same hold, raises no alarm.
	clock := first.Add(3 * time.Hour)
	dispatch.Now = func() time.Time { return clock }
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatal(err)
	}
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker}
	if alarms, err := survey.Starved(ctx, clock); err != nil || len(alarms) != 0 {
		t.Errorf("expected no starved alarm under the usage-limit hold, got %+v %v", alarms, err)
	}
}

func TestARigHeldByAnotherHarnessStopOrDamperIsStillStarved(t *testing.T) {
	ctx := context.Background()
	// A rate limit's hold is not a usage limit: it still names the harness stop.
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Memory = tracker
	first := time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return first }
	hold := first.Add(40*time.Hour).Format(time.RFC3339) + " " + string(application.ReasonRateLimit) + " rate limited"
	if err := tracker.SetNote(ctx, application.HarnessHoldKey("vps"), hold); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatal(err)
	}
	kept, _ := tracker.Note(ctx, application.StarvedKey("vps"))
	if records := application.ParseStarved(kept); len(records) != 1 || records[0].Hold != "harness stop" {
		t.Errorf("expected the rate limit's hold kept as a starved rig held by the harness stop, got %q", kept)
	}

	// The same two stories under the smoke hold and no usage limit still are.
	dispatch, tracker, _ = aSmokeHeldRig(t)
	dispatch.Now = func() time.Time { return first }
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatal(err)
	}
	kept, _ = tracker.Note(ctx, application.StarvedKey("vps"))
	if records := application.ParseStarved(kept); len(records) != 1 || records[0].Hold != "smoke hold" {
		t.Errorf("expected the smoke-held rig kept as starved, got %q", kept)
	}
}

// aHeldHostFixture is a tracker holding rig millwright's ready stories pinned
// to desktop, whose note says its harness is held (mw-gq6.785), and one story
// of the rig pinned to each of others.
func aHeldHostFixture(t *testing.T, others ...string) *apptest.FakeTracker {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.1", "One", "desktop")
	storyOn(t, tracker, "mw-gq6.2", "Two", "desktop")
	for n, host := range others {
		storyOn(t, tracker, "mw-gq6."+string(rune('3'+n)), "Elsewhere", host)
	}
	note := application.StarvedRig{
		Rig: "millwright", Since: statusNow.Add(-2 * time.Hour), Hold: application.StarvedHolds["harness-hold"],
		Reason: "desktop claims nothing new until 22:00Z: the harness stopped a session",
	}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("desktop"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	return tracker
}

func TestAHeldHarnessAlarmNamesTheCommandThatRoutesEachReadyStoryToAnotherHost(t *testing.T) {
	alarms := starvedAlarms(t, aHeldHostFixture(t, "laptop"))
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm, got %+v", alarms)
	}
	text := alarms[0].Text
	for _, want := range []string{"mw path set mw-gq6.1 host=laptop", "mw path set mw-gq6.2 host=laptop"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the alarm, got %q", want, text)
		}
	}
	if strings.Contains(text, "host=laptop mw-gq6.3") || strings.Contains(text, "mw path set mw-gq6.3") {
		t.Errorf("expected no command for the story already on laptop, got %q", text)
	}
	if strings.Contains(text, "\n") {
		t.Errorf("expected one line, got %q", text)
	}
}

// closedTracker is a tracker that also lists what was closed, as the real
// gateway does (VerifyBeads.ClosedStories): the ids it is given are read back
// with ShowBeads.
type closedTracker struct {
	*apptest.FakeTracker
	closed []application.ClosedStory
	err    error
	// shown collects the ids ShowBeads was asked for; nil leaves them uncounted.
	shown *[]string
	// asked collects the limit each rig list was given; nil leaves them uncounted.
	asked *[]int
}

func (c closedTracker) ShowBeads(ctx context.Context, ids []string) ([]application.StoryDetail, error) {
	if c.shown != nil {
		*c.shown = append(*c.shown, ids...)
	}
	return c.FakeTracker.ShowBeads(ctx, ids)
}

// ClosedStoriesOfRig answers as the gateway does: the rig's stories closed since,
// newest first, at most limit of them, each with the Path it carries.
func (c closedTracker) ClosedStoriesOfRig(_ context.Context, rig string, since time.Time, limit int) ([]application.ClosedStory, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.asked != nil {
		*c.asked = append(*c.asked, limit)
	}
	var out []application.ClosedStory
	for _, s := range c.closed {
		if s.Rig == rig && !s.ClosedAt.Before(since) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClosedAt.After(out[j].ClosedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c closedTracker) ClosedStories(_ context.Context, since time.Time) ([]application.ClosedStory, error) {
	if c.err != nil {
		return nil, c.err
	}
	var out []application.ClosedStory
	for _, s := range c.closed {
		if !s.ClosedAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

// heldAlarmText is the one alarm text of a rig held on desktop, read from the
// tracker.
func heldAlarmText(t *testing.T, tracker application.WorkTracker, notes application.DamperNotes) string {
	t.Helper()
	alarms, err := application.DamperSurvey{Tracker: tracker, Notes: notes}.Starved(context.Background(), statusNow)
	if err != nil {
		t.Fatalf("reading the starved rigs: %v", err)
	}
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm, got %+v", alarms)
	}
	return alarms[0].Text
}

// aClosedStoryOn closes a story of the rig that ran on host, ago before statusNow.
func aClosedStoryOn(t *testing.T, tracker *apptest.FakeTracker, id, host string, ago time.Duration) application.ClosedStory {
	t.Helper()
	storyOn(t, tracker, id, "Landed", host)
	if err := tracker.CloseStory(context.Background(), id, "landed"); err != nil {
		t.Fatal(err)
	}
	return application.ClosedStory{ID: id, Title: "Landed", ClosedAt: statusNow.Add(-ago), Rig: "millwright", Host: host}
}

// 2026-10-25: the desktop's harness was held, millwright had two ready stories
// routed there, the laptop had no open millwright work but had landed one that
// morning, and the alarm said no other host runs millwright.
func TestAHeldHarnessAlarmNamesTheHostThatRanTheRigThroughAStoryNowClosed(t *testing.T) {
	tracker := aHeldHostFixture(t)
	landed := aClosedStoryOn(t, tracker, "mw-gq6.9", "laptop", 6*time.Hour)
	text := heldAlarmText(t, closedTracker{FakeTracker: tracker, closed: []application.ClosedStory{landed}}, tracker)
	for _, want := range []string{"laptop", "mw path set mw-gq6.1 host=laptop", "mw path set mw-gq6.2 host=laptop"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the alarm, got %q", want, text)
		}
	}
	if strings.Contains(text, "no other host") {
		t.Errorf("expected no claim that no other host runs the rig, got %q", text)
	}
	if strings.Contains(text, "\n") {
		t.Errorf("expected one line, got %q", text)
	}
}

func TestAHeldHarnessAlarmWithNoOtherHostSaysSoAndStatesTheWindow(t *testing.T) {
	tracker := aHeldHostFixture(t)
	// A story of the held host itself, closed before the note's Since (a story
	// closed after it is a landing that clears the alarm), and one closed before
	// the window.
	mine := aClosedStoryOn(t, tracker, "mw-gq6.8", "desktop", 3*time.Hour)
	old := aClosedStoryOn(t, tracker, "mw-gq6.9", "laptop", application.StarvedRanWindow+time.Hour)
	text := heldAlarmText(t, closedTracker{FakeTracker: tracker, closed: []application.ClosedStory{mine, old}}, tracker)
	if !strings.Contains(text, "no other host has run millwright in the last 14 days") {
		t.Errorf("expected the alarm to say no other host has run millwright in the last 14 days, got %q", text)
	}
	if strings.Contains(text, "mw path set") {
		t.Errorf("expected no command with no other host, got %q", text)
	}
}

func TestAHeldHarnessAlarmNeverClaimsNoOtherHostWhenClosedWorkCannotBeRead(t *testing.T) {
	for name, tracker := range map[string]application.WorkTracker{
		"a tracker that lists no closed stories": aHeldHostFixture(t),
		"a closed list that fails":               closedTracker{FakeTracker: aHeldHostFixture(t), err: errors.New("the vault is out of reach")},
	} {
		text := heldAlarmText(t, tracker, tracker.(interface {
			NotesWithPrefix(context.Context, string) (map[string]string, error)
		}))
		if strings.Contains(text, "no other host") {
			t.Errorf("%s: expected no claim that no other host runs the rig, got %q", name, text)
		}
		if !strings.Contains(text, "Way out: ") || !strings.Contains(text, "could not be read") {
			t.Errorf("%s: expected the way out to say the hosts could not be read, got %q", name, text)
		}
	}
}

func TestAHeldHarnessAlarmListsEveryOtherHostAndRoutesToTheFirst(t *testing.T) {
	text := starvedAlarms(t, aHeldHostFixture(t, "vps", "laptop"))[0].Text
	for _, want := range []string{"laptop, vps", "mw path set mw-gq6.1 host=laptop"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the alarm, got %q", want, text)
		}
	}
}

func TestAStarvedRigHeldForAnotherReasonCarriesNoWayOutLine(t *testing.T) {
	if text := starvedAlarms(t, starvedFixture(t, 4*time.Hour))[0].Text; strings.Contains(text, "mw path set") || strings.Contains(text, "no other host") {
		t.Errorf("expected no way-out line for a smoke hold, got %q", text)
	}
}

// 2026-10-25: mw-gq6.786 listed and showed every story closed in 14 days (about
// 23,000) whenever a host was harness-held, and mw doctor, mw status and mw stuck
// ran for minutes. The read is the held rig's newest stories, and none is shown.
func TestAHeldHarnessAlarmReadsOnlyTheHeldRigsNewestClosedStories(t *testing.T) {
	tracker := aHeldHostFixture(t)
	var closed []application.ClosedStory
	for i := 0; i < 5000; i++ {
		rig, host := fmt.Sprintf("rig%d", i%4), "desktop"
		if i%7 == 0 {
			host = "vps"
		}
		closed = append(closed, application.ClosedStory{
			ID: fmt.Sprintf("mw-bulk.%d", i), Rig: rig, Host: host, ClosedAt: statusNow.Add(-time.Duration(i+1) * time.Minute),
		})
	}
	// The held rig's newest hundred ran on desktop, then one on the laptop, then
	// thousands more on desktop; all of them before the note's Since (2h ago), as a
	// story closed after it would clear the alarm.
	for i := 0; i < 5100; i++ {
		ago := 2*time.Hour + time.Duration(i+1)*30*time.Second
		if i >= 100 {
			ago += time.Hour
		}
		closed = append(closed, application.ClosedStory{ID: fmt.Sprintf("mw-old.%d", i), Rig: "millwright", Host: "desktop", ClosedAt: statusNow.Add(-ago)})
	}
	closed = append(closed, application.ClosedStory{ID: "mw-gq6.9", Rig: "millwright", Host: "laptop", ClosedAt: statusNow.Add(-3 * time.Hour)})
	var shown []string
	var asked []int
	text := heldAlarmText(t, closedTracker{FakeTracker: tracker, closed: closed, shown: &shown, asked: &asked}, tracker)
	if !strings.Contains(text, "laptop") || strings.Contains(text, "no other host") {
		t.Errorf("expected the alarm to name laptop, got %q", text)
	}
	if len(shown) > 50 {
		t.Errorf("expected at most 50 beads shown, got %d", len(shown))
	}
	if len(asked) != 1 || asked[0] <= 0 || asked[0] > 200 {
		t.Errorf("expected one bounded list of the held rig, got limits %v", asked)
	}
}

func TestAStarvedRigStaysAnAlarmWhenItsOnlyClaimedStoryIsQueuedForItsBatch(t *testing.T) {
	tracker := starvedFixture(t, 4*time.Hour)
	ctx := context.Background()
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryState(ctx, "mw-gq6.1", application.RunState, application.RunQueued, "the test"); err != nil {
		t.Fatal(err)
	}
	if alarms := starvedAlarms(t, tracker); len(alarms) != 1 {
		t.Errorf("expected the alarm kept: a story queued for its batch has no session, got %+v", alarms)
	}
}

// mw-gq6.868: the alarm is one fact whichever host sees it. Two hosts' doctors
// read the same notes and work, so they raise the same alarm seconds apart; the
// Mayor is mailed once, and again only when the alarm changes or has cleared.
func aStarvedRigCheckOnHost(host string, at time.Time, tracker *apptest.FakeTracker, mailbox *apptest.FakeMailbox) *application.StarvedRigCheck {
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker}
	return &application.StarvedRigCheck{
		Host:   host,
		Alarms: func(ctx context.Context) ([]application.DamperAlarm, error) { return survey.Starved(ctx, at) },
		Mail:   func(ctx context.Context, m application.NewMessage) error { _, err := mailbox.Send(ctx, m); return err },
		Mailed: tracker,
	}
}

func runDoctorsOfTwoHosts(t *testing.T, tracker *apptest.FakeTracker, mailbox *apptest.FakeMailbox) {
	t.Helper()
	ctx := context.Background()
	for i, host := range []string{"laptop", "desktop"} {
		now := doctorNow
		doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now,
			aStarvedRigCheckOnHost(host, statusNow.Add(time.Duration(7*i)*time.Second), tracker, mailbox))
		if _, err := doctor.Run(ctx, "", false); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
	}
}

func TestTwoHostsRaisingTheSameStarvedAlarmMailTheMayorOnce(t *testing.T) {
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	runDoctorsOfTwoHosts(t, tracker, mailbox)
	inbox, _ := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(inbox) != 1 {
		t.Fatalf("expected one mail for the one alarm two hosts raised, got %d: %+v", len(inbox), inbox)
	}
}

func TestAChangedStarvedAlarmIsMailedAgain(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	runDoctorsOfTwoHosts(t, tracker, mailbox)

	// Another story of the rig is ready: a different alarm.
	storyOn(t, tracker, "mw-gq6.3", "Three", "vps")
	runDoctorsOfTwoHosts(t, tracker, mailbox)
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Fatalf("expected a second mail for another ready story, got %d", len(inbox))
	}

	// Another hold of the same rig and stories is the same alarm (mw-gq6.1194).
	note := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: "no room", Reason: "load 23.9 of 20"}
	if err := tracker.SetNote(ctx, application.StarvedKey("vps"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	runDoctorsOfTwoHosts(t, tracker, mailbox)
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Fatalf("expected no mail for another hold of the same alarm, got %d", len(inbox))
	}
}

func TestAStarvedAlarmClearedAndRaisedAgainIsMailedAgain(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	runDoctorsOfTwoHosts(t, tracker, mailbox)

	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	runDoctorsOfTwoHosts(t, tracker, mailbox) // cleared: a story of the rig runs
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	runDoctorsOfTwoHosts(t, tracker, mailbox) // raised again, the same rig, stories and hold
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Fatalf("expected one mail for each time the alarm was raised, got %d", len(inbox))
	}
}

func TestADryRunOfTheStarvedCheckClearsNothingForTheOtherHosts(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	runDoctorsOfTwoHosts(t, tracker, mailbox)
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	if _, err := doctor.Run(ctx, "", true); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	runDoctorsOfTwoHosts(t, tracker, mailbox)
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 1 {
		t.Fatalf("expected a dry run to leave what was mailed as it was, got %d mails", len(inbox))
	}
}

// mw-gq6.870: a rig held only by load (no room on the host) waits on the host,
// which is the Mayor's to know and never the Governor's to be paged for. It is
// mailed to the Mayor once and the check stays ok, so no faulty verdict, no
// damped note and no failed unit goes beyond the Mayor; a rig held by a fault
// is a fault still.
func aLoadHeldRig(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := starvedFixture(t, 4*time.Hour)
	note := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: application.StarvedHolds["no-room"], Reason: "laptop has no room: load 34.3 of 20 cores"}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("vps"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	return tracker
}

func TestAStarvedAlarmHeldOnlyByLoadIsAWaitAndAFaultIsNot(t *testing.T) {
	load := starvedAlarms(t, aLoadHeldRig(t))
	if len(load) != 1 || !load[0].Wait {
		t.Errorf("expected one alarm that is a wait for room, got %+v", load)
	}
	fault := starvedAlarms(t, starvedFixture(t, 4*time.Hour))
	if len(fault) != 1 || fault[0].Wait {
		t.Errorf("expected a smoke hold to be no wait, got %+v", fault)
	}
}

func TestAStarvedAlarmHeldOnlyByLoadMailsTheMayorAndIsNeverAFault(t *testing.T) {
	ctx := context.Background()
	tracker := aLoadHeldRig(t)
	mailbox := apptest.NewFakeMailbox()
	for i, host := range []string{"laptop", "desktop"} {
		now := doctorNow
		doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now,
			aStarvedRigCheckOnHost(host, statusNow.Add(time.Duration(7*i)*time.Second), tracker, mailbox))
		// Ticks later it is the same wait: never faulty, never damped.
		for tick := range 4 {
			report, err := doctor.Run(ctx, "", false)
			if err != nil || report.Faulty() || report.Results[0].Verdict != "ok" {
				t.Fatalf("%s tick %d: expected a wait for room to leave the check ok, got %+v %v", host, tick, report.Results, err)
			}
			now = now.Add(5 * time.Minute)
		}
	}
	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "load 34.3 of 20 cores") || !strings.Contains(inbox[0].Body, "no room") {
		t.Fatalf("expected the Mayor mailed once of the wait for room, got %+v", inbox)
	}
}

func TestAStarvedAlarmHeldByLoadIsMailedAgainOnceItClearedAndIsRaisedAgain(t *testing.T) {
	ctx := context.Background()
	tracker := aLoadHeldRig(t)
	mailbox := apptest.NewFakeMailbox()
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	run := func() {
		t.Helper()
		if _, err := doctor.Run(ctx, "", false); err != nil {
			t.Fatal(err)
		}
	}
	run()
	run()
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	run() // cleared: a story of the rig runs
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	run()
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Fatalf("expected one mail for each time the wait was raised, got %d", len(inbox))
	}
}

func TestADryRunOfAWaitForRoomMailsNothing(t *testing.T) {
	ctx := context.Background()
	tracker := aLoadHeldRig(t)
	mailbox := apptest.NewFakeMailbox()
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	if _, err := doctor.Run(ctx, "", true); err != nil {
		t.Fatal(err)
	}
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 0 {
		t.Fatalf("expected a dry run to mail nothing, got %d", len(inbox))
	}
}

func TestAStarvedAlarmHeldByAFaultStaysAFaultOnceTheMayorIsMailed(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	if _, err := doctor.Run(ctx, "", false); err != nil {
		t.Fatalf("expected the first tick to cure by mailing, got %v", err)
	}
	report, err := doctor.Run(ctx, "", false)
	if err == nil || !report.Faulty() {
		t.Fatalf("expected the fault to be left faulty after its one mail, got %+v %v", report.Results, err)
	}
}

// mw-gq6.984: one starving of a rig is one mail while it only waits. A hold that
// flips between no room and the cap a tutor in use puts on the host (both waits
// for the host) is the same wait; a cap with no tutor in use, or any other hold,
// is a fault as it was.
const (
	tutorCapReason = "laptop has taken 4 of the 3 sessions it may run at once while a tutor is in use (grist first)"
	plainCapReason = "laptop has taken 3 of the 3 sessions it may run at once"
	// zeroCapReason is a cap of 0, which no running Builder ends: mw-gq6.1050.
	zeroCapReason = "laptop has taken 0 of the 0 sessions it may run at once"
)

func holdRigBy(t *testing.T, tracker *apptest.FakeTracker, rig, hold, reason string) {
	t.Helper()
	note := application.StarvedRig{Rig: rig, Since: statusNow.Add(-4 * time.Hour), Hold: hold, Reason: reason}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("vps"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
}

func aWaitingRigCheck(t *testing.T) (*apptest.FakeTracker, *apptest.FakeMailbox, func()) {
	t.Helper()
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	return tracker, mailbox, func() {
		t.Helper()
		if _, err := doctor.Run(ctx, "", false); err != nil {
			t.Fatal(err)
		}
	}
}

func mailsToTheMayor(t *testing.T, mailbox *apptest.FakeMailbox) []application.Message {
	t.Helper()
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	return inbox
}

func TestACapHeldByATutorInUseOrBuildersAloneIsAWaitAndACapOfZeroIsAFault(t *testing.T) {
	tracker := starvedFixture(t, 4*time.Hour)
	holdRigBy(t, tracker, "millwright", "cap", tutorCapReason)
	if alarms := starvedAlarms(t, tracker); len(alarms) != 1 || !alarms[0].Wait {
		t.Errorf("expected the cap a tutor in use puts on the host to be a wait, got %+v", alarms)
	}
	holdRigBy(t, tracker, "millwright", "cap", plainCapReason)
	if alarms := starvedAlarms(t, tracker); len(alarms) != 1 || !alarms[0].Wait || !strings.Contains(alarms[0].Text, "held by cap") {
		t.Errorf("expected a full cap of running Builders to be a wait still named as the hold, got %+v", alarms)
	}
	holdRigBy(t, tracker, "millwright", "cap", zeroCapReason)
	if alarms := starvedAlarms(t, tracker); len(alarms) != 1 || alarms[0].Wait {
		t.Errorf("expected a cap of 0 to be a fault, got %+v", alarms)
	}
}

// mw-gq6.1050: the Laptop at its cap of 3 with Builders, then no room, then the
// cap again, was three mails in 45 minutes.
func TestAHoldFlippingBetweenTheCapAndNoRoomMailsTheMayorOnce(t *testing.T) {
	tracker, mailbox, run := aWaitingRigCheck(t)
	for _, step := range []struct{ hold, reason string }{
		{"cap", plainCapReason},
		{"no room", "laptop has no room: load 24.3 of 20 cores"},
		{"cap", plainCapReason},
	} {
		holdRigBy(t, tracker, "millwright", step.hold, step.reason)
		run()
	}
	inbox := mailsToTheMayor(t, mailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "held by cap") {
		t.Fatalf("expected one mail naming the cap for the episode, got %d: %+v", len(inbox), inbox)
	}
}

func TestACapOfZeroIsAFaultButTheSameStarvingAsTheWaitBeforeIt(t *testing.T) {
	tracker, mailbox, run := aWaitingRigCheck(t)
	holdRigBy(t, tracker, "millwright", "no room", "laptop has no room: load 24.3 of 20 cores")
	run()
	holdRigBy(t, tracker, "millwright", "cap", zeroCapReason)
	run()
	if inbox := mailsToTheMayor(t, mailbox); len(inbox) != 1 {
		t.Fatalf("expected the one starving mailed once, got %d", len(inbox))
	}
	zero := starvedAlarms(t, tracker)
	holdRigBy(t, tracker, "millwright", "cap", plainCapReason)
	if now := starvedAlarms(t, tracker); len(zero) != 1 || len(now) != 1 || zero[0].Wait || !now[0].Wait || zero[0].Identity != now[0].Identity {
		t.Errorf("expected a cap of 0 to be a fault and a cap of Builders a wait of one identity, got %+v and %+v", zero, now)
	}
}

func TestAHoldFlippingBetweenNoRoomAndTheTutorCapMailsTheMayorOnce(t *testing.T) {
	tracker, mailbox, run := aWaitingRigCheck(t)
	for _, step := range []struct{ hold, reason string }{
		{"no room", "laptop has no room: load 34.3 of 20 cores"},
		{"cap", tutorCapReason},
		{"no room", "laptop has no room: load 34.3 of 20 cores"},
		{"no room", "laptop has no room: load 21.0 of 20 cores"},
	} {
		holdRigBy(t, tracker, "millwright", step.hold, step.reason)
		run()
	}
	if inbox := mailsToTheMayor(t, mailbox); len(inbox) != 1 {
		t.Fatalf("expected one mail for one starving of one rig, got %d: %+v", len(inbox), inbox)
	}
}

func TestAFlippingWaitIsMailedAgainOnceAStoryOfTheRigStartedAndItStarvesAgain(t *testing.T) {
	ctx := context.Background()
	tracker, mailbox, run := aWaitingRigCheck(t)
	holdRigBy(t, tracker, "millwright", "no room", "laptop has no room: load 34.3 of 20 cores")
	run()
	holdRigBy(t, tracker, "millwright", "cap", tutorCapReason)
	run()
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	run() // cleared: a story of the rig runs
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	holdRigBy(t, tracker, "millwright", "cap", tutorCapReason)
	run()
	if inbox := mailsToTheMayor(t, mailbox); len(inbox) != 2 {
		t.Fatalf("expected one mail for each starving, got %d", len(inbox))
	}
}

func TestASecondRigStarvingWhileTheFirstWaitsIsMailed(t *testing.T) {
	tracker, mailbox, run := aWaitingRigCheck(t)
	holdRigBy(t, tracker, "millwright", "no room", "laptop has no room: load 34.3 of 20 cores")
	run()
	other := domain.Story{ID: "mw-gq6.7", Title: "Other rig"}
	if err := other.Overrides.Set("host", "vps"); err != nil {
		t.Fatal(err)
	}
	if err := other.Overrides.Set("rig", "leaderboard"); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-gq6", other)
	notes := []application.StarvedRig{
		{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: "cap", Reason: tutorCapReason},
		{Rig: "leaderboard", Since: statusNow.Add(-4 * time.Hour), Hold: "no room", Reason: "laptop has no room: load 34.3 of 20 cores"},
	}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("vps"), application.FormatStarved(notes)); err != nil {
		t.Fatal(err)
	}
	run()
	inbox := mailsToTheMayor(t, mailbox)
	if len(inbox) != 2 || !strings.Contains(inbox[1].Body, "leaderboard") {
		t.Fatalf("expected a second mail naming the second rig, got %+v", inbox)
	}
}

func TestAFlipFromAWaitToAFaultIsNotMailedAgainAndTheFaultStaysAFault(t *testing.T) {
	ctx := context.Background()
	tracker, mailbox, run := aWaitingRigCheck(t)
	holdRigBy(t, tracker, "millwright", "cap", tutorCapReason)
	run()
	holdRigBy(t, tracker, "millwright", "cap", zeroCapReason)
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	if _, err := doctor.Run(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if report, err := doctor.Run(ctx, "", false); err == nil || !report.Faulty() {
		t.Fatalf("expected the fault to stay faulty after its mail, got %+v %v", report.Results, err)
	}
	if inbox := mailsToTheMayor(t, mailbox); len(inbox) != 1 {
		t.Fatalf("expected the one mail of the wait, got %d", len(inbox))
	}
}

// aLoadHeldFixture is the rig of mw-gq6.1049: millwright's two stories ready, and
// the note desktop left of the rig held by load since ago.
func aLoadHeldFixture(t *testing.T, ago time.Duration) *apptest.FakeTracker {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.1", "One", "desktop")
	storyOn(t, tracker, "mw-gq6.2", "Two", "desktop")
	note := application.StarvedRig{
		Rig: "millwright", Since: statusNow.Add(-ago), Hold: application.StarvedHolds["no-room"],
		Reason: "desktop has no room: load 28.2 of 16 cores",
	}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("desktop"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	return tracker
}

// desktopDispatchedAgo leaves the ticks note desktop's sync writes, its last good
// dispatch run ago before statusNow.
func desktopDispatchedAgo(t *testing.T, tracker *apptest.FakeTracker, ago time.Duration) {
	t.Helper()
	held := application.HostTicks{Dispatch: application.TickCount{Known: true, LastGood: statusNow.Add(-ago)}}
	if err := tracker.SetNote(context.Background(), application.TicksKey("desktop"), held.Note()); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.1049, 2026-11-07: the desktop at cap 0 kept a note of millwright held by
// load since 2h21m before, and the alarm mailed it while millwright stories
// landed on the laptop.
func TestARigWithAStoryLandedOnAnyHostSinceTheNotesSinceIsNotStarved(t *testing.T) {
	tracker := aLoadHeldFixture(t, 2*time.Hour)
	landed := aClosedStoryOn(t, tracker, "mw-gq6.9", "laptop", 10*time.Minute)
	alarms, err := application.DamperSurvey{Tracker: closedTracker{FakeTracker: tracker, closed: []application.ClosedStory{landed}}, Notes: tracker}.Starved(context.Background(), statusNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(alarms) != 0 {
		t.Errorf("expected no alarm for a rig that landed a story on the laptop 10m ago, got %+v", alarms)
	}
}

func TestARigThatLandedAStoryBeforeTheNotesSinceIsStillStarved(t *testing.T) {
	tracker := aLoadHeldFixture(t, 2*time.Hour)
	landed := aClosedStoryOn(t, tracker, "mw-gq6.9", "laptop", 3*time.Hour)
	alarms, err := application.DamperSurvey{Tracker: closedTracker{FakeTracker: tracker, closed: []application.ClosedStory{landed}}, Notes: tracker}.Starved(context.Background(), statusNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(alarms) != 1 {
		t.Errorf("expected the alarm for a rig whose last landing was before it was held, got %+v", alarms)
	}
}

func TestARigHeldByAHostThatHasMadeNoPassForOverTheTimerPeriodIsNotAlarmed(t *testing.T) {
	stale := application.StarvedPassStale
	if stale <= application.DispatchTimerPeriod {
		t.Fatalf("expected the stale limit %s to be over the dispatch timer's period %s", stale, application.DispatchTimerPeriod)
	}
	tracker := aLoadHeldFixture(t, 2*time.Hour)
	desktopDispatchedAgo(t, tracker, stale+time.Minute)
	if alarms := starvedAlarms(t, tracker); len(alarms) != 0 {
		t.Errorf("expected no alarm from a note of a host not passing, got %+v", alarms)
	}
}

func TestARigHeldByAHostThatPassedWithinTheTimerPeriodIsStillAlarmed(t *testing.T) {
	tracker := aLoadHeldFixture(t, 2*time.Hour)
	desktopDispatchedAgo(t, tracker, application.StarvedPassStale-time.Minute)
	if alarms := starvedAlarms(t, tracker); len(alarms) != 1 {
		t.Errorf("expected the alarm from a host that is passing, got %+v", alarms)
	}
}

func TestAHostWithNoGoodDispatchRunInItsTicksNoteIsNotPassing(t *testing.T) {
	tracker := aLoadHeldFixture(t, 2*time.Hour)
	held := application.HostTicks{Dispatch: application.TickCount{Known: true, Failed: 3}}
	if err := tracker.SetNote(context.Background(), application.TicksKey("desktop"), held.Note()); err != nil {
		t.Fatal(err)
	}
	if alarms := starvedAlarms(t, tracker); len(alarms) != 0 {
		t.Errorf("expected no alarm from a host whose dispatch never ran well, got %+v", alarms)
	}
}

// A host's other note being fresh does not keep a dead host's note alive: the
// fresh host's own hold is judged, the dead host's is not.
func TestAStaleHostsNoteDoesNotHideAnotherHostsFreshHold(t *testing.T) {
	tracker := aLoadHeldFixture(t, 3*time.Hour)
	desktopDispatchedAgo(t, tracker, 5*time.Hour)
	note := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-90 * time.Minute), Hold: "smoke hold", Reason: "the smoke failed"}
	if err := tracker.SetNote(context.Background(), application.StarvedKey("vps"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
		t.Fatal(err)
	}
	alarms := starvedAlarms(t, tracker)
	if len(alarms) != 1 || !strings.Contains(alarms[0].Text, "smoke hold") || !strings.Contains(alarms[0].Text, "1h30m") {
		t.Errorf("expected the vps's own hold, held 1h30m, got %+v", alarms)
	}
}

// mw-gq6.1127: the starved note of a box the cloud destroyed is the old box's.
func TestAStarvedNoteOfADestroyedBoxHoldsNoRigBackButOneOfALiveBoxDoes(t *testing.T) {
	for name, tc := range map[string]struct {
		boxes []application.CloudBox
		want  int
	}{
		"destroyed": {nil, 0},
		"making":    {[]application.CloudBox{{Name: "cloud1", Making: true, Up: statusNow.Add(-time.Hour)}}, 0},
		"up":        {[]application.CloudBox{{Name: "cloud1", Up: statusNow.Add(-5 * time.Hour), Checked: statusNow.Add(-4 * time.Hour)}}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.1", "One", "vps")
			note := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: "smoke hold", Reason: "the grist smoke failed"}
			if err := tracker.SetNote(context.Background(), application.StarvedKey("cloud1"), application.FormatStarved([]application.StarvedRig{note})); err != nil {
				t.Fatal(err)
			}
			book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: tc.boxes}}
			alarms, err := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book, CloudPlan: cloudPlan}.Starved(context.Background(), statusNow)
			if err != nil {
				t.Fatal(err)
			}
			if len(alarms) != tc.want {
				t.Errorf("expected %d starved alarms, got %+v", tc.want, alarms)
			}
		})
	}
}

// mw-gq6.1194: trade-tracker's one ready story was mailed three times in 20
// minutes as its hold flipped between no room (a wait) and no grist key (a
// fault). A starved rig is one mail an episode, whatever its hold flips to.
func TestStarvedMailsOncePerEpisodeWhenTheHoldFlips(t *testing.T) {
	ctx := context.Background()
	tracker := starvedFixture(t, 4*time.Hour)
	mailbox := apptest.NewFakeMailbox()
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, aStarvedRigCheckOnHost("laptop", statusNow, tracker, mailbox))
	// A fault stays faulty after its mail, so a tick on one ends in an error that
	// is not what is tested here.
	tick := func() {
		t.Helper()
		_, _ = doctor.Run(ctx, "", false)
	}
	noRoom := func() { holdRigBy(t, tracker, "millwright", "no room", "laptop has no room: load 18.2 of 16 cores") }
	noKey := func() { holdRigBy(t, tracker, "millwright", "no grist key", "laptop has no grist key") }
	mails := func() int { return len(mailsToTheMayor(t, mailbox)) }
	for _, step := range []func(){noRoom, noKey, noRoom} {
		step()
		tick()
	}
	if got := mails(); got != 1 {
		t.Fatalf("expected one mail for one starving whose hold flipped, got %d: %+v", got, mailsToTheMayor(t, mailbox))
	}

	// The same flips the other way round, from a fault.
	noKey()
	tick()
	noRoom()
	tick()
	if got := mails(); got != 1 {
		t.Fatalf("expected the same episode still one mail, got %d", got)
	}

	// A story of the rig starts, then the rig starves again: a second mail.
	if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	tick()
	if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	noKey()
	tick()
	noRoom()
	tick()
	if got := mails(); got != 2 {
		t.Fatalf("expected a second mail for the next episode, got %d", got)
	}

	// The newest hold and reason are what the alarm (and so mw status) shows.
	alarms := starvedAlarms(t, tracker)
	if len(alarms) != 1 || !strings.Contains(alarms[0].Text, "held by no room — laptop has no room: load 18.2 of 16 cores") {
		t.Errorf("expected the alarm to show the newest hold and reason, got %+v", alarms)
	}
}
