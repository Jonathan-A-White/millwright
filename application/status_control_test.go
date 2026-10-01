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

func controlAt(ago time.Duration, bead, actor, detail string) events.Event {
	return events.Event{Ts: statusNow.Add(-ago), Kind: events.KindControl, Bead: bead, Actor: actor, Detail: detail, Lane: events.LaneNormal}
}

// mw status names the runs cancelled in the last day and a host that is
// paused, from the event log, and leaves both out when there are none.
func TestStatusShowsCancelledRunsAndAPausedHost(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	log := &apptest.FakeEventLog{}
	for _, ev := range []events.Event{
		controlAt(30*time.Hour, "mw-old", "governor@postern", "cancel"),
		controlAt(2*time.Hour, "mw-gq6.1", "governor@postern", "cancel"),
		controlAt(time.Hour, "", "mw@laptop", "pause-host vps"),
	} {
		if _, err := log.Append(context.Background(), []events.Event{ev}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := application.Status{
		Tracker: tracker, Host: "vps", Seat: "builder", Control: log,
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	for _, want := range []string{"CANCELLED (1)", "mw-gq6.1", "cancelled by governor@postern", "PAUSED", "mw@laptop"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the report lacks %q:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "mw-old") {
		t.Errorf("a cancel from 30 hours ago is shown:\n%s", printed)
	}

	quiet, err := application.Status{Tracker: tracker, Host: "vps", Seat: "builder", Control: &apptest.FakeEventLog{}, Now: func() time.Time { return statusNow }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := quiet.String(); strings.Contains(s, "CANCELLED") || strings.Contains(s, "PAUSED") {
		t.Errorf("a quiet log still shows a section:\n%s", s)
	}
}
