package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// longControlLog is a log of busy job events with a few control events among
// them, and what the readers must still find in it.
func longControlLog(t *testing.T) *apptest.FakeEventLog {
	t.Helper()
	log := &apptest.FakeEventLog{}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	var evs []events.Event
	add := func(ago time.Duration, e events.Event) {
		e.Ts, e.Lane = now.Add(-ago), events.LaneNormal
		evs = append(evs, e)
	}
	add(30*time.Hour, events.Event{Kind: events.KindControl, Bead: "mw-old", Actor: "governor@postern", Detail: "cancel"})
	add(29*time.Hour, events.Event{Kind: events.KindControl, Actor: "mw@laptop", Detail: "pause-host vps"})
	for i := 0; i < 3000; i++ {
		add(20*time.Hour, events.Event{Kind: events.KindJob, Actor: fmt.Sprintf("dispatch@h%d", i%7), From: events.JobScheduled, To: events.JobRunning})
	}
	add(5*time.Hour, events.Event{Kind: events.KindControl, Bead: "mw-a", Actor: "governor@postern", Detail: "cancel"})
	add(4*time.Hour, events.Event{Kind: events.KindControl, Actor: "mw@laptop", Detail: "resume-host vps"})
	add(3*time.Hour, events.Event{Kind: events.KindControl, Bead: "mw-b", Actor: "mw@laptop", Detail: "cancel"})
	add(2*time.Hour, events.Event{Kind: events.KindControl, Actor: "mw@laptop", Detail: "pause-host laptop"})
	add(time.Hour, events.Event{Kind: events.KindControl, Bead: "mw-a", Actor: "mw@vps", Detail: "cancel"})
	for _, e := range evs {
		if _, err := log.Append(context.Background(), []events.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

// PausedHost and CancelsSince answer from the log's kept newest events: they
// do not scan a log that grows with every pass (mw-gq6.659).
func TestPausedHostAndCancelsSinceDoNotScanTheLog(t *testing.T) {
	log := longControlLog(t)
	ctx := context.Background()

	pause, paused, err := application.PausedHost(ctx, log, "laptop")
	if err != nil || !paused || pause.Actor != "mw@laptop" {
		t.Fatalf("laptop: got %+v paused=%v err=%v, want paused by mw@laptop", pause, paused, err)
	}
	if _, paused, err = application.PausedHost(ctx, log, "vps"); err != nil || paused {
		t.Fatalf("vps was resumed: paused=%v err=%v", paused, err)
	}
	if _, paused, err = application.PausedHost(ctx, log, "cloud"); err != nil || paused {
		t.Fatalf("cloud never paused: paused=%v err=%v", paused, err)
	}

	since := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC).Add(-24 * time.Hour)
	cancels, err := application.CancelsSince(ctx, log, since)
	if err != nil {
		t.Fatal(err)
	}
	if scanned := log.Scanned(); scanned != 0 {
		t.Fatalf("the readers scanned %d events of the log, want none", scanned)
	}
	if len(cancels) != 2 || cancels[0].Bead != "mw-b" || cancels[1].Bead != "mw-a" || cancels[1].Actor != "mw@vps" {
		t.Fatalf("got %+v, want mw-b then the later cancel of mw-a by mw@vps (mw-old is before the window)", cancels)
	}

}
