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
