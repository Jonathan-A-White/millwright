package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1394: a box takes the name of one destroyed before it, and a hold kept
// under the name must not leave the new box claiming nothing, billed idle.

type failingClear struct{ *apptest.FakeTracker }

func (failingClear) ClearNote(context.Context, string) error { return errors.New("bd is down") }

func aMakeUnderHold(t *testing.T, notes application.CloudHolds, key, value string) (string, *apptest.FakeEventLog) {
	t.Helper()
	now := time.Date(2026, 11, 16, 18, 25, 0, 0, time.UTC)
	if tracker, ok := notes.(*apptest.FakeTracker); ok && value != "" {
		_ = tracker.SetNote(context.Background(), key, value)
	}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud2", Up: now, Making: true}}}}
	log := &apptest.FakeEventLog{}
	var said bytes.Buffer
	err := application.CloudMake{Provider: &apptest.FakeCloud{}, Book: book, Holds: notes, Log: log, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }, Out: &said}.Run(context.Background(), "cloud2")
	if err != nil {
		t.Fatal(err)
	}
	return said.String(), log
}

func TestAMakeClearsTheHarnessStopAnOlderBoxOfTheNameLeft(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	said, _ := aMakeUnderHold(t, tracker, application.HarnessHoldKey("cloud2"), "2026-11-20T00:00:00Z usage-limit You've hit your weekly limit")
	if got, _ := tracker.Note(context.Background(), application.HarnessHoldKey("cloud2")); got != "" {
		t.Errorf("expected the old box's hold cleared, still %q", got)
	}
	if !strings.Contains(said, "cloud2: cleared the harness hold an older box of that name left") {
		t.Errorf("expected the make to say it cleared the hold, got:\n%s", said)
	}
}

func TestAMakeKeepsTheGovernorsHoldAndSaysHowToClearIt(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	value := "2027-01-01T00:00:00Z governor laptop-only: no new claims on cloud boxes"
	said, _ := aMakeUnderHold(t, tracker, application.HarnessHoldKey("cloud2"), value)
	if got, _ := tracker.Note(context.Background(), application.HarnessHoldKey("cloud2")); got != value {
		t.Errorf("expected the Governor's hold kept, got %q", got)
	}
	for _, want := range []string{"the Governor's hold stands", "laptop-only", "bd kv clear host.cloud2.harness_hold"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the make to say %q, got:\n%s", want, said)
		}
	}
}

func TestAMakeClearsAHarnessStopWhoseTimeHasPassed(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aMakeUnderHold(t, tracker, application.HarnessHoldKey("cloud2"), "2026-11-14T00:00:00Z rate-limit slow down")
	if got, _ := tracker.Note(context.Background(), application.HarnessHoldKey("cloud2")); got != "" {
		t.Errorf("expected a spent hold cleared too, still %q", got)
	}
}

func TestAMakeSaysAHoldItCouldNotClear(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	_ = tracker.SetNote(context.Background(), application.HarnessHoldKey("cloud2"), "2026-11-20T00:00:00Z usage-limit hit")
	said, _ := aMakeUnderHold(t, failingClear{tracker}, "", "")
	if !strings.Contains(said, "could not be cleared") || !strings.Contains(said, "bd kv clear host.cloud2.harness_hold") {
		t.Errorf("expected the make to say the hold stands and how to clear it, got:\n%s", said)
	}
}

func TestAMakeSaysTheDrainStandingOnTheName(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	said, _ := aMakeUnderHold(t, tracker, application.DrainedKey("cloud2"), "2026-11-14T00:00:00Z the cloud is drained by the Governor")
	if got, _ := tracker.Note(context.Background(), application.DrainedKey("cloud2")); got == "" {
		t.Errorf("expected the drained hold kept: only undrain clears it")
	}
	if !strings.Contains(said, "the cloud's drain stands on this name") || !strings.Contains(said, "mw cloud undrain") {
		t.Errorf("expected the make to name the drain, got:\n%s", said)
	}
}

func TestAMakeWithNoNotesSaysNothingOfHolds(t *testing.T) {
	said, _ := aMakeUnderHold(t, nil, "", "")
	if strings.Contains(said, "hold") {
		t.Errorf("expected no word of holds with no notes to read, got:\n%s", said)
	}
}

func TestStatusNamesOnlyAGovernorHoldThatStillStands(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 19, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	_ = tracker.SetNote(ctx, application.HarnessHoldKey("cloud2"), "2027-01-01T00:00:00Z governor laptop-only")
	_ = tracker.SetNote(ctx, application.HarnessHoldKey("cloud3"), "2027-01-01T00:00:00Z usage-limit hit")
	_ = tracker.SetNote(ctx, application.HarnessHoldKey("cloud4"), "2026-11-01T00:00:00Z governor long ago")
	reading := &application.CloudReading{Boxes: []application.CloudBox{{Name: "cloud2"}, {Name: "cloud3"}, {Name: "cloud4"}}, Now: now}
	reading.ReadHolds(ctx, tracker, now)
	if len(reading.Holds) != 1 || reading.Holds[0].Box != "cloud2" {
		t.Fatalf("expected only cloud2's hold named, got %+v", reading.Holds)
	}
	(*application.CloudReading)(nil).ReadHolds(ctx, tracker, now)
}
