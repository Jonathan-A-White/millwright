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

// mw-gq6.1296: the Governor drains the cloud to go back to the laptop alone:
// the boxes finish what they run and claim nothing new, and no box is made.

func aCloudToDrain(t *testing.T, host, home string, maxBoxes int, boxes ...string) (application.CloudDrain, *apptest.FakeCloudDrainFiles, *apptest.FakeTracker) {
	t.Helper()
	files := &apptest.FakeCloudDrainFiles{MaxBoxes: maxBoxes}
	tracker := apptest.NewFakeTracker()
	state := application.CloudState{}
	for _, name := range boxes {
		state.Boxes = append(state.Boxes, application.CloudBox{Name: name, Up: time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)})
	}
	return application.CloudDrain{
		Files: files,
		Book:  &apptest.FakeCloudBook{State: state},
		Notes: tracker,
		Home:  &apptest.FakeHomeFile{Text: home + " 2026-10-15T17:03:00Z mw@" + home},
		Host:  host,
		Now:   func() time.Time { return time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC) },
	}, files, tracker
}

func TestDrainSetsMaxBoxesToZeroKeepsTheOldValueAndHoldsEachBoxUp(t *testing.T) {
	ctx := context.Background()
	drain, files, tracker := aCloudToDrain(t, "laptop", "laptop", 4, "cloud2", "cloud3")

	report, err := drain.Drain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(files.Writes, ","); got != "drain 4" {
		t.Fatalf("expected max_boxes 0 with the old 4 kept, got %q", got)
	}
	for _, box := range []string{"cloud2", "cloud3"} {
		hold, held := application.ReadDrainedHold(ctx, tracker, box)
		if !held || hold.Reason != application.ReasonDrained {
			t.Errorf("expected %s on a drained hold of its own, got %+v (held %v)", box, hold, held)
		}
		if note, _ := tracker.Note(ctx, application.HarnessHoldKey(box)); note != "" {
			t.Errorf("expected %s given no harness hold: the harness stopped nothing", box)
		}
	}
	for _, want := range []string{"max_boxes is 0 (was 4)", "cloud2", "cloud3", "Way back: mw cloud undrain"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("expected the report to say %q, got:\n%s", want, report)
		}
	}
}

func TestASecondDrainKeepsTheFirstOldValue(t *testing.T) {
	ctx := context.Background()
	drain, files, tracker := aCloudToDrain(t, "laptop", "laptop", 4, "cloud2")
	if _, err := drain.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	drain.Book = &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud2"}, {Name: "cloud5"}}}}
	report, err := drain.Drain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if files.Kept != 4 || files.MaxBoxes != 0 {
		t.Fatalf("expected the first old value 4 kept and max_boxes 0, got kept %d, max_boxes %d", files.Kept, files.MaxBoxes)
	}
	if got := strings.Join(files.Writes, ","); got != "drain 4" {
		t.Errorf("expected the files written once, got %q", got)
	}
	if _, held := application.ReadDrainedHold(ctx, tracker, "cloud5"); !held {
		t.Errorf("expected a box up since the first drain held by the second")
	}
	if !strings.Contains(report.String(), "already drained") {
		t.Errorf("expected the report to say the cloud was already drained, got:\n%s", report)
	}
}

func TestUndrainPutsMaxBoxesBackAndClearsTheDrainedHolds(t *testing.T) {
	ctx := context.Background()
	drain, files, tracker := aCloudToDrain(t, "laptop", "laptop", 4, "cloud2", "cloud3")
	if _, err := drain.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	// A box destroyed idle while drained leaves its hold: it is cleared too, so
	// that the next box of that name is not born held.
	drain.Book = &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud3"}}}}

	report, err := drain.Undrain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if files.MaxBoxes != 4 || files.Drained || files.Kept != 0 {
		t.Fatalf("expected max_boxes back at 4, got max_boxes %d kept %d drained %v", files.MaxBoxes, files.Kept, files.Drained)
	}
	for _, box := range []string{"cloud2", "cloud3"} {
		if _, held := application.ReadDrainedHold(ctx, tracker, box); held {
			t.Errorf("expected the drained hold of %s cleared", box)
		}
	}
	if !strings.Contains(report.String(), "max_boxes is 4 (was 0)") {
		t.Errorf("expected the report to say max_boxes is back, got:\n%s", report)
	}
}

func TestUndrainOnACloudThatIsNotDrainedSaysSoAndChangesNothing(t *testing.T) {
	ctx := context.Background()
	drain, files, tracker := aCloudToDrain(t, "laptop", "laptop", 4, "cloud2")

	report, err := drain.Undrain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Writes) != 0 || files.MaxBoxes != 4 {
		t.Errorf("expected no file written, got %v, max_boxes %d", files.Writes, files.MaxBoxes)
	}
	if tracker.NoteWrites(application.DrainedKey("cloud2")) != 0 {
		t.Errorf("expected no note written")
	}
	if !strings.Contains(report.String(), "not drained") {
		t.Errorf("expected the report to say the cloud is not drained, got:\n%s", report)
	}
}

func TestDrainAndUndrainRefuseOnAHostThatIsNotHome(t *testing.T) {
	ctx := context.Background()
	drain, files, tracker := aCloudToDrain(t, "desktop", "laptop", 4, "cloud2")

	if _, err := drain.Drain(ctx); err == nil || !strings.Contains(err.Error(), "this host is not home") {
		t.Errorf("expected drain refused as not home, got %v", err)
	}
	if _, err := drain.Undrain(ctx); err == nil || !strings.Contains(err.Error(), "this host is not home") {
		t.Errorf("expected undrain refused as not home, got %v", err)
	}
	if len(files.Writes) != 0 || tracker.NoteWrites(application.DrainedKey("cloud2")) != 0 {
		t.Errorf("expected nothing written, got %v", files.Writes)
	}
}

func TestADrainedHostClaimsNothingNewAndSaysTheCloudIsDrained(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	now := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	dispatch.Memory = tracker
	dispatch.Cap = 2
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story ready to be worked"})
	if err := tracker.SetNote(ctx, application.DrainedKey("vps"), "2026-11-14T00:00:00Z drained by the Governor"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Started) != 0 {
		t.Fatalf("expected nothing started on a drained host, got %+v", report.Started)
	}
	var why string
	for _, passed := range report.Passed {
		if passed.StoryID == "mw-gq6.1" {
			why = passed.Why
		}
	}
	if !strings.Contains(why, "vps claims nothing new: the cloud is drained by the Governor") {
		t.Errorf("expected the passed-over line to say the cloud is drained, got %q", why)
	}
	if strings.Contains(why, "harness") || strings.Contains(report.String(), "the harness stopped") {
		t.Errorf("expected no word of a harness stop, got %q\n%s", why, report)
	}
	if note, _ := tracker.Note(ctx, application.HarnessHoldKey("vps")); note != "" {
		t.Errorf("expected no harness hold written")
	}
}

func TestStatusSaysADrainedHostIsHeldByTheDrainAndNotByTheHarness(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	if err := tracker.SetNote(ctx, application.DrainedKey("cloud2"), "2026-11-14T00:00:00Z drained by the Governor"); err != nil {
		t.Fatal(err)
	}
	hold, held := application.ReadHostHold(ctx, tracker, "cloud2", time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC))
	if !held {
		t.Fatal("expected the host held")
	}
	said := application.StatusReport{Host: "cloud2", HarnessHold: &hold}.String()
	if !strings.Contains(said, "the cloud is drained by the Governor") || !strings.Contains(said, "cloud2 claims nothing new") || strings.Contains(said, "harness") {
		t.Errorf("expected mw status to say the cloud is drained and not the harness, got:\n%s", said)
	}
}

func TestADrainedHostStopsNoSessionItIsRunning(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	now := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	dispatch.Cap = 2
	claimedStory(t, tracker, "mw-gq6.9", 1)
	name := startOldSession(t, runner, "mw-gq6.9")
	if err := tracker.SetNote(ctx, application.DrainedKey("vps"), "2026-11-14T00:00:00Z drained by the Governor"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Running != 1 || len(runner.Closed()) != 0 {
		t.Errorf("expected the running session counted and left alone, got running %d, closed %v", report.Running, runner.Closed())
	}
	if status, _ := runner.Status(ctx, name); !status.Running() {
		t.Errorf("expected the session still alive")
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-gq6.9"); detail.Assignee == "" {
		t.Errorf("expected the claim kept")
	}
}

func TestADrainedBoxThatIsIdleIsStillDestroyedAtIdleMinutes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	if err := tracker.SetNote(ctx, application.DrainedKey("cloud1"), "2026-11-14T00:00:00Z drained by the Governor"); err != nil {
		t.Fatal(err)
	}
	plan := application.CloudPlan{MaxBoxes: 0, MonthlyCapUSD: 75, HourlyUSD: 0.132, Idle: 30 * time.Minute, BoxCap: 2}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{
		{Name: "cloud1", Up: now.Add(-3 * time.Hour), IdleSince: now.Add(-plan.Idle)}}}}
	_, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Notes: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: plan, Now: func() time.Time { return now },
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(book.State.Boxes) != 0 {
		t.Errorf("expected the idle drained box destroyed at idle_minutes, got %+v", book.State.Boxes)
	}
}
