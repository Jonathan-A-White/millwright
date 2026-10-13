package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// mw-gq6.319: a rig whose last grist smoke failed is passed over by dispatch,
// its story left open and unclaimed, and goes on once the smoke passes.
func TestDispatchHoldsTheOpenStoriesOfARigWhoseGristSmokeFailed(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	book := application.GristSmokeBook{Notes: tracker, Host: "laptop"}
	dispatch.SmokeHolds = book

	failed := application.GristSmokeReport{App: "cairn", Examples: 1, Failures: []string{"cairn/sweep/one: the mill refused it: no licence"}}
	if err := book.Record(ctx, "millwright", failed); err != nil {
		t.Fatalf("recording the smoke: %v", err)
	}
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("expected nothing started under the hold, got %+v", report.Started)
	}
	if len(report.Passed) != 1 || !strings.Contains(report.Passed[0].Why, "grist smoke of cairn failed") {
		t.Fatalf("expected the story passed over for the failed smoke, saying why, got %+v", report.Passed)
	}
	if detail, err := tracker.ShowStory(ctx, "mw-gq6.1"); err != nil || detail.Assignee != "" {
		t.Fatalf("expected the held story left unclaimed, got %q %v", detail.Assignee, err)
	}

	passed := application.GristSmokeReport{App: "cairn", Examples: 1}
	if err := book.Record(ctx, "", passed); err != nil {
		t.Fatalf("recording the smoke that passed: %v", err)
	}
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching after the smoke passed: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("expected the story started once the smoke passed, got %+v passed %+v", report.Started, report.Passed)
	}
}

// A failure run by hand keeps the rig an earlier failure held, and a kind-less
// app is forgotten, not recorded.
func TestAFailureByHandKeepsTheRigItHeldAndAnAppWithNoGrindsIsForgotten(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	book := application.GristSmokeBook{Notes: tracker, Host: "laptop"}
	failed := application.GristSmokeReport{App: "cairn", Examples: 1, Failures: []string{"x"}}
	if err := book.Record(ctx, "cairn", failed); err != nil {
		t.Fatal(err)
	}
	if err := book.Record(ctx, "", failed); err != nil {
		t.Fatal(err)
	}
	if why, _ := book.HeldBy(ctx, "cairn"); why == "" {
		t.Fatal("expected the hold kept by a failure by hand")
	}
	if err := book.Record(ctx, "", application.GristSmokeReport{App: "cairn", NoGrinds: true}); err != nil {
		t.Fatal(err)
	}
	if records, _ := book.Records(ctx); len(records) != 0 {
		t.Fatalf("expected an app with no grinds forgotten, got %+v", records)
	}
}

// mw-gq6.331: a failed smoke is the Mayor's to act on, not the Governor's: its
// event is on the normal lane, and its text is one phone-sized line that names
// the rig, the time, how many examples were wrong and the hold, with no
// example's own error in it. The record keeps every error for mw status.
func TestAFailedGristSmokePostsOneShortLineOnTheNormalLane(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	log := &apptest.FakeEventLog{}
	at := time.Date(2026, 10, 10, 2, 48, 9, 0, time.UTC)
	book := application.GristSmokeBook{Notes: tracker, Events: log, Host: "laptop", Now: func() time.Time { return at }}

	failed := application.GristSmokeReport{App: "legend", Examples: 3, Failures: []string{
		"legend/sweep/one: the mill refused it: " + strings.Repeat("no licence ", 60),
		"legend/sweep/two: the answer lacked a price",
	}}
	if err := book.Record(ctx, "legend", failed); err != nil {
		t.Fatalf("recording the smoke: %v", err)
	}
	all := log.All()
	if len(all) != 1 {
		t.Fatalf("expected one event, got %v", all)
	}
	ev := all[0]
	if ev.Lane != events.LaneNormal {
		t.Fatalf("expected the failure on the normal lane, got %q", ev.Lane)
	}
	want := "grist smoke: legend FAILED 2026-10-10 02:48Z: 2 examples wrong, the open stories of legend are held; details in mw status"
	if ev.Detail != want {
		t.Fatalf("expected the one line %q, got %q", want, ev.Detail)
	}
	if strings.Contains(ev.Detail, "\n") || strings.Contains(ev.Detail, "licence") || strings.Contains(ev.Detail, "price") {
		t.Fatalf("expected no example's error in the text, got %q", ev.Detail)
	}

	records, err := book.Records(ctx)
	if err != nil || len(records) != 1 || !records[0].Failed || len(records[0].Failures) != 2 || !strings.Contains(records[0].Line(), "no licence") {
		t.Fatalf("expected the record to keep the hold and every error for mw status, got %+v %v", records, err)
	}
}

// mw-gq6.338: a smoke that fails exactly as the last record did is recorded and
// not posted again; only ok (or none) -> FAILED, or a changed set of failing
// examples, is news.
func TestARepeatGristSmokeFailurePostsNoSecondAlarm(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	log := &apptest.FakeEventLog{}
	at := time.Date(2026, 10, 10, 2, 46, 0, 0, time.UTC)
	book := application.GristSmokeBook{Notes: tracker, Events: log, Host: "laptop", Now: func() time.Time { return at }}
	failing := func(failures ...string) application.GristSmokeReport {
		return application.GristSmokeReport{App: "legend", Examples: 6, Failures: failures}
	}
	record := func(report application.GristSmokeReport) {
		t.Helper()
		if err := book.Record(ctx, "legend", report); err != nil {
			t.Fatalf("recording the smoke: %v", err)
		}
	}

	record(application.GristSmokeReport{App: "legend", Examples: 6})
	record(failing("legend/sweep/one: no answer in 5m0s", "legend/sweep/two: bad"))
	if n := len(log.All()); n != 1 {
		t.Fatalf("expected ok -> FAILED to post one event, got %d", n)
	}

	at = at.Add(45 * time.Minute)
	record(failing("legend/sweep/two: bad", "legend/sweep/one: no answer in 5m0s"))
	if n := len(log.All()); n != 1 {
		t.Fatalf("expected the same failures again to post nothing, got %d events", n)
	}
	records, err := book.Records(ctx)
	if err != nil || len(records) != 1 || !records[0].At.Equal(at) || !records[0].Failed {
		t.Fatalf("expected the repeat to be recorded with its new time, got %+v %v", records, err)
	}

	record(failing("legend/sweep/one: no answer in 5m0s"))
	if n := len(log.All()); n != 2 {
		t.Fatalf("expected a different set of failures to post again, got %d events", n)
	}
	record(failing("legend/sweep/one: no answer in 5m0s"))
	if n := len(log.All()); n != 2 {
		t.Fatalf("expected a repeat to post nothing, got %d events", n)
	}

	record(application.GristSmokeReport{App: "legend", Examples: 6})
	record(failing("legend/sweep/one: no answer in 5m0s"))
	if n := len(log.All()); n != 3 {
		t.Fatalf("expected ok -> FAILED after a pass to post again, got %d events", n)
	}
}

// mw-gq6.412: a rig held by a failed smoke still dispatches the open bead that
// names the failing examples (its fix must be able to run); every other story
// of the rig stays held. Once the bead is closed it names nothing.
func TestAHeldRigStillDispatchesTheBeadThatNamesTheFailingExamples(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Host, dispatch.Cap = "vps", 5
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Some other story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "The fix"})
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.2", map[string]string{application.SmokeExamplesField: "cairn/sweep/one, cairn/sweep/two"}); err != nil {
		t.Fatal(err)
	}
	book := application.GristSmokeBook{Notes: tracker, Host: "vps"}
	dispatch.SmokeHolds = book
	failed := application.GristSmokeReport{App: "cairn", Examples: 3, Failures: []string{"cairn/sweep/one: the answer lacked a price", "cairn/sweep/three: the answer lacked a price"}}
	if err := book.Record(ctx, "millwright", failed); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.2" {
		t.Fatalf("expected only the bead that names a failing example started, got %+v passed %+v", report.Started, report.Passed)
	}
	if len(report.Passed) != 1 || report.Passed[0].StoryID != "mw-gq6.1" || !strings.Contains(report.Passed[0].Why, "grist smoke of cairn failed") {
		t.Fatalf("expected the other story passed over for the smoke, got %+v", report.Passed)
	}
}

// A failure whose examples are all named on an open bead is recorded as filed
// and holds nothing; the alarm, which would say it holds, is not posted.
func TestAFailureFiledOnAnOpenBeadHoldsNothingAndPostsNoAlarm(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	log := &apptest.FakeEventLog{}
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "The bug"})
	_ = tracker.SetStoryMetadata(ctx, "mw-gq6.9", map[string]string{application.SmokeExamplesField: "cairn/sweep/one"})
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Events: log, Host: "laptop"}

	report := book.Mark(ctx, application.GristSmokeReport{App: "cairn", Examples: 2, Failures: []string{"cairn/sweep/one: wrong"}})
	if line := report.Line(); !strings.Contains(line, "FAILED 1 of 2 examples, all filed on mw-gq6.9: not held") {
		t.Fatalf("expected the line to say it is filed, got %q", line)
	}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	if why, _ := book.HeldBy(ctx, "millwright"); why != "" {
		t.Fatalf("expected nothing held, got %q", why)
	}
	if len(log.All()) != 0 {
		t.Fatalf("expected no alarm, got %v", log.All())
	}
	// A failure that is not named at all holds.
	other := book.Mark(ctx, application.GristSmokeReport{App: "cairn", Examples: 2, Failures: []string{"cairn/sweep/one: wrong", "the mill refused it: no licence"}})
	if len(other.FiledOn) != 0 {
		t.Fatalf("expected a failure with a part unnamed not filed, got %v", other.FiledOn)
	}
	// Closing the bead ends it.
	_ = tracker.CloseStory(ctx, "mw-gq6.9", "fixed")
	closed := book.Mark(ctx, application.GristSmokeReport{App: "cairn", Examples: 2, Failures: []string{"cairn/sweep/one: wrong"}})
	if len(closed.FiledOn) != 0 {
		t.Fatalf("expected a closed bead to name nothing, got %v", closed.FiledOn)
	}
}

// mw-gq6.412: naming examples completes <kind>/<example> with the app, refuses
// an example of another app or a closed bead, and adds to what a bead names.
func TestNamingFailingExamplesOnABead(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "The bug"})
	file := application.GristSmokeFile{Tracker: tracker, Book: application.GristSmokeBook{Notes: tracker, Filed: tracker}}

	named, err := file.Run(ctx, "mw-gq6.9", "cairn", []string{"sweep/one"})
	if err != nil || strings.Join(named, ",") != "cairn/sweep/one" {
		t.Fatalf("expected the example completed with its app, got %v %v", named, err)
	}
	named, err = file.Run(ctx, "mw-gq6.9", "cairn", []string{"cairn/sweep/two", "sweep/one"})
	if err != nil || strings.Join(named, ",") != "cairn/sweep/one,cairn/sweep/two" {
		t.Fatalf("expected the examples added to those named, each once, got %v %v", named, err)
	}
	if got := tracker.Metadata("mw-gq6.9")[application.SmokeExamplesField]; got != "cairn/sweep/one,cairn/sweep/two" {
		t.Fatalf("expected the field written, got %q", got)
	}
	if _, err := file.Run(ctx, "mw-gq6.9", "cairn", []string{"legend/sweep/one"}); err == nil {
		t.Fatal("expected an example of another app refused")
	}
	if _, err := file.Run(ctx, "mw-gq6.9", "cairn", []string{"one"}); err == nil {
		t.Fatal("expected a bare example name refused")
	}
	_ = tracker.CloseStory(ctx, "mw-gq6.9", "fixed")
	if _, err := file.Run(ctx, "mw-gq6.9", "cairn", []string{"sweep/three"}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a closed bead refused, got %v", err)
	}
}
