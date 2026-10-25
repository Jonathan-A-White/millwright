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

// mw-gq6.579: the bug filed as the cause of a failing smoke names the failing
// example in its title or description, and is not held by the failure it is the
// fix of, though nobody named the example in its smoke_examples metadata. The
// dispatch log says it ran over the hold, and why.
func TestAHeldRigStillDispatchesTheBugWhoseTextNamesAFailingExample(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Host, dispatch.Cap = "vps", 5
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Some other story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "[bug] A request answer carries a setting_offer"})
	if err := tracker.SetDescription("mw-gq6.2", "The smoke fails on cairn/sweep/one: setting_offer wanted absent."); err != nil {
		t.Fatal(err)
	}
	book := application.GristSmokeBook{Notes: tracker, Host: "vps"}
	dispatch.SmokeHolds = book
	failed := application.GristSmokeReport{App: "cairn", Examples: 3, Failures: []string{"cairn/sweep/one: setting_offer wanted absent"}}
	if err := book.Record(ctx, "millwright", failed); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.2" {
		t.Fatalf("expected the bug that names the failing example started, got %+v passed %+v", report.Started, report.Passed)
	}
	if !strings.Contains(report.String(), "runs over the grist smoke hold of millwright") || !strings.Contains(report.String(), "cairn/sweep/one") {
		t.Fatalf("expected the dispatch log to say the hold was passed and why, got %q", report.String())
	}
	if len(report.Passed) != 1 || report.Passed[0].StoryID != "mw-gq6.1" {
		t.Fatalf("expected the other story still held, got %+v", report.Passed)
	}
}

// mw-gq6.579: --filed <bead> with no example names the failing examples of the
// app's last smoke, and a fix that names only some of them is on the line mw
// status shows.
func TestFilingABeadWithNoExamplesNamesWhatTheLastSmokeFailedOn(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "The bug"})
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Host: "vps"}
	file := application.GristSmokeFile{Tracker: tracker, Book: book}

	if _, err := file.Run(ctx, "mw-gq6.9", "cairn", nil); err == nil {
		t.Fatal("expected a refusal when the app has no failing smoke and no example is named")
	}
	failed := application.GristSmokeReport{App: "cairn", Examples: 3, Failures: []string{
		"cairn/sweep/one: wrong", "cairn/sweep/two: wrong", "the mill refused it: no licence"}}
	if err := book.Record(ctx, "millwright", failed); err != nil {
		t.Fatal(err)
	}
	named, err := file.Run(ctx, "mw-gq6.9", "cairn", nil)
	if err != nil || strings.Join(named, ",") != "cairn/sweep/one,cairn/sweep/two" {
		t.Fatalf("expected the failing examples named, got %v %v", named, err)
	}
	records, err := book.Records(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected one record, got %v %v", records, err)
	}
	// The refusal is no example's, so the failure is still held; the fix is on the line.
	if why, _ := book.HeldBy(ctx, "millwright"); why == "" {
		t.Fatal("expected the failure with a part unnamed still held")
	}
	if line := records[0].Line(); !strings.Contains(line, "mw-gq6.9 runs over the hold") {
		t.Fatalf("expected mw status to name the fix that runs over the hold, got %q", line)
	}
}

// mw-gq6.696: a failure whose every failing example an open bead names holds
// nothing, and the miss is added to that bead as a dated note; a failure with
// an example no bead names holds and writes no note.
func TestAFailureEveryExampleOfWhichIsNamedIsNotedOnTheNamingBead(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "The fix"})
	_ = tracker.SetStoryMetadata(ctx, "mw-gq6.2", map[string]string{application.SmokeExamplesField: "cairn/sweep/one, cairn/sweep/two"})
	at := time.Date(2026, 10, 22, 1, 56, 0, 0, time.UTC)
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Host: "laptop", Now: func() time.Time { return at }}

	all := application.GristSmokeReport{App: "cairn", Examples: 2, Failures: []string{"cairn/sweep/one: the answer lacked a price", "cairn/sweep/two: no answer in 5m0s"}}
	if err := book.Record(ctx, "legend", all); err != nil {
		t.Fatal(err)
	}
	comments := tracker.Comments("mw-gq6.2")
	if len(comments) != 2 {
		t.Fatalf("expected a note for each failing example, got %q", comments)
	}
	for i, want := range []string{"cairn/sweep/one: the answer lacked a price", "cairn/sweep/two: no answer in 5m0s"} {
		if !strings.Contains(comments[i], "2026-10-22") || !strings.Contains(comments[i], want) || !strings.Contains(comments[i], "already named on mw-gq6.2") {
			t.Errorf("expected a dated note of %q saying it is already named, got %q", want, comments[i])
		}
	}

	some := application.GristSmokeReport{App: "cairn", Examples: 2, Failures: []string{"cairn/sweep/one: bad", "cairn/sweep/three: bad"}}
	if err := book.Record(ctx, "legend", some); err != nil {
		t.Fatal(err)
	}
	if got := tracker.Comments("mw-gq6.2"); len(got) != 2 {
		t.Errorf("expected no note for a failure that holds, got %q", got)
	}
}

// mw-gq6.773: an open bead whose title or description names a failing example,
// by its bare name or as <app>/<kind>/<example>, already names it: the failure
// holds nothing, posts no alarm, names the bead, and the miss is noted on it.
func TestAFailureWhoseExampleAnOpenBeadsTextNamesIsFiledOnIt(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	log := &apptest.FakeEventLog{}
	tracker.AddStory("mw-fu7dqf", domain.Story{ID: "mw-fu7dqf.14", Title: "[bug] the new example verse-text-ask fails live 2/2"})
	tracker.AddStory("mw-fu7dqf", domain.Story{ID: "mw-fu7dqf.15", Title: "[bug] the answer to a request"})
	if err := tracker.SetDescription("mw-fu7dqf.15", "It is lampas/tutor/hello that misses."); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-fu7dqf", domain.Story{ID: "mw-fu7dqf.16", Title: "Another app's example is wrong: legend/bible-talk/verse-text-ask"})
	tracker.AddStory("mw-fu7dqf", domain.Story{ID: "mw-fu7dqf.17", Title: "verse-text-ask-two is wrong"})
	at := time.Date(2026, 10, 25, 8, 2, 0, 0, time.UTC)
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Events: log, Host: "laptop", Now: func() time.Time { return at }}

	report := book.Mark(ctx, application.GristSmokeReport{App: "lampas", Examples: 137, Failures: []string{"lampas/bible-talk/verse-text-ask: wrong"}})
	if line := report.Line(); !strings.Contains(line, "all filed on mw-fu7dqf.14: not held") {
		t.Fatalf("expected the bead naming the example in its title, got %q", line)
	}
	if err := book.Record(ctx, "lampas", report); err != nil {
		t.Fatal(err)
	}
	if why, _ := book.HeldBy(ctx, "lampas"); why != "" {
		t.Fatalf("expected nothing held, got %q", why)
	}
	if len(log.All()) != 0 {
		t.Fatalf("expected no alarm, got %v", log.All())
	}
	if got := tracker.Comments("mw-fu7dqf.14"); len(got) != 1 || !strings.Contains(got[0], "already named on mw-fu7dqf.14") {
		t.Fatalf("expected the miss noted on the bead, got %q", got)
	}

	full := book.Mark(ctx, application.GristSmokeReport{App: "lampas", Examples: 2, Failures: []string{"lampas/tutor/hello: wrong"}})
	if len(full.FiledOn) != 1 || full.FiledOn[0] != "mw-fu7dqf.15" {
		t.Fatalf("expected the bead naming the full name in its description, got %v", full.FiledOn)
	}

	// Another app's example, or a longer name that merely starts with it, is not it.
	for _, failure := range []string{"lampas/bible-talk/verse-text", "lampas/bible-talk/verse-text-as"} {
		if other := book.Mark(ctx, application.GristSmokeReport{App: "lampas", Failures: []string{failure + ": wrong"}}); len(other.FiledOn) != 0 {
			t.Fatalf("expected %s named by no bead, got %v", failure, other.FiledOn)
		}
	}
	_ = tracker.CloseStory(ctx, "mw-fu7dqf.14", "fixed")
	if closed := book.Mark(ctx, application.GristSmokeReport{App: "lampas", Failures: []string{"lampas/bible-talk/verse-text-ask: wrong"}}); len(closed.FiledOn) != 0 {
		t.Fatalf("expected a closed bead to name nothing, got %v", closed.FiledOn)
	}
}

// mw-gq6.773: the forms of an example's name a bead's text may use.
func TestNamesSmokeExampleReadsTheBareNameAndTheFullNameAtTheirEdges(t *testing.T) {
	const example = "lampas/bible-talk/verse-text-ask"
	for text, want := range map[string]bool{
		"the new example verse-text-ask fails live 2/2": true,
		"lampas/bible-talk/verse-text-ask":              true,
		"(verse-text-ask), verse-text-ask.":             true,
		"verse-text-ask-two fails":                      false,
		"the verse-text-asked":                          false,
		"my-verse-text-ask":                             false,
		"legend/bible-talk/verse-text-ask":              false,
		"nothing here":                                  false,
	} {
		if got := application.NamesSmokeExample(text, example); got != want {
			t.Errorf("NamesSmokeExample(%q) = %v, want %v", text, got, want)
		}
	}
}
