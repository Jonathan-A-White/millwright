package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// mw-gq6.1435: a pause-host for a box's name, mirrored as a note, outlived the box
// it was set for: a new box of the name was paused for as long as the event stood.

func aMakeUnderAPause(t *testing.T, pausedAt time.Time) (*apptest.FakeTracker, *apptest.FakeEventLog, string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 18, 25, 0, 0, time.UTC)
	_, tracker, _, runner, _ := aFactory(t)
	log := &apptest.FakeEventLog{}
	if _, err := log.Append(ctx, []events.Event{{Ts: pausedAt, Kind: events.KindControl, Actor: "governor@postern", Lane: events.LaneNormal, Detail: "pause-host cloud2"}}); err != nil {
		t.Fatal(err)
	}
	mirror := &application.EventControl{Log: log, Cursors: &apptest.FakeNudgeCursors{}, Tracker: tracker, Runner: runner, Host: "laptop", Notes: tracker}
	if err := mirror.Control(ctx); err != nil {
		t.Fatal(err)
	}
	if _, paused, _ := application.PausedByNote(ctx, tracker, "cloud2"); !paused {
		t.Fatal("the pause was not mirrored: the fixture is wrong")
	}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud2", Up: now, Making: true}}}}
	var said bytes.Buffer
	err := application.CloudMake{Provider: &apptest.FakeCloud{}, Book: book, Holds: tracker, Log: log, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }, Out: &said}.Run(ctx, "cloud2")
	if err != nil {
		t.Fatal(err)
	}
	return tracker, log, said.String()
}

func TestAPauseOfADestroyedBoxDoesNotHoldTheNewBoxOfItsName(t *testing.T) {
	ctx := context.Background()
	tracker, log, said := aMakeUnderAPause(t, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	if _, paused, _ := application.PausedByNote(ctx, tracker, "cloud2"); paused {
		t.Errorf("expected the old box's pause note cleared as a box of its name is made")
	}
	if _, paused, err := application.PausedHost(ctx, log, "cloud2"); err != nil || paused {
		t.Errorf("expected the log to say cloud2 resumed, paused=%v err=%v", paused, err)
	}
	if !strings.Contains(said, "cloud2: cleared the pause an older box of that name left") {
		t.Errorf("expected the make to say it cleared the pause, got:\n%s", said)
	}
}

func TestAPauseSetWhileTheBoxIsBeingMadeStands(t *testing.T) {
	ctx := context.Background()
	tracker, log, _ := aMakeUnderAPause(t, time.Date(2026, 11, 16, 18, 26, 0, 0, time.UTC))
	if _, paused, _ := application.PausedByNote(ctx, tracker, "cloud2"); !paused {
		t.Errorf("expected a pause set after the box was asked for to stand")
	}
	if _, paused, _ := application.PausedHost(ctx, log, "cloud2"); !paused {
		t.Errorf("expected a pause set after the box was asked for to stand in the log")
	}
}
