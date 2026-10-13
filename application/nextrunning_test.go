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

// mw-gq6.394: a by-hand mw next on a story whose session is still running
// refuses and changes nothing.
func TestARunningSessionIsNotClosedOutByHand(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	marks := apptest.NewFakeCloseOutMarks()
	beats := apptest.NewFakeHeartbeats()
	beats.Start("mw-gq6.1", 4242)
	var out bytes.Buffer

	report, err := application.Next{
		Tracker:    tracker,
		Heartbeats: beats,
		CloseOuts:  marks,
		Out:        &out,
	}.Run(context.Background(), "mw-gq6.1")

	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	for _, want := range []string{"mw-gq6.1", application.SessionName("mw-gq6.1"), "still running", "4242", "nothing was touched"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
	if report.Landed || report.Closed {
		t.Errorf("expected nothing done, got %+v", report)
	}
	if len(marks.Writes) != 0 {
		t.Errorf("expected no close-out mark, got %+v", marks.Writes)
	}
	if out.Len() != 0 {
		t.Errorf("expected nothing printed, got %q", out.String())
	}
	if len(tracker.Comments("mw-gq6.1")) != 0 || len(tracker.Asked()) != 0 {
		t.Errorf("expected the tracker untouched, got comments %q and asked %v", tracker.Comments("mw-gq6.1"), tracker.Asked())
	}
}

// A heartbeat of another story, or none, or a table that cannot be read, is no
// reason to refuse.
func TestAClosedOutStoryIsOnlyRefusedForItsOwnHeartbeat(t *testing.T) {
	other := apptest.NewFakeHeartbeats()
	other.Start("mw-gq6.2", 7)
	unreadable := apptest.NewFakeHeartbeats()
	unreadable.Err = errors.New("no /proc")
	for name, beats := range map[string]application.LiveHeartbeats{"another story's": other, "none": apptest.NewFakeHeartbeats(), "unreadable": unreadable} {
		marks := apptest.NewFakeCloseOutMarks()
		_, _ = application.Next{Tracker: apptest.NewFakeTracker(), Heartbeats: beats, CloseOuts: marks}.Run(context.Background(), "mw-gq6.1")
		if len(marks.Writes) == 0 {
			t.Errorf("%s heartbeat: expected the close-out to go on, and it never began", name)
		}
	}
}

// The heartbeat itself, the session's own close-out of the lease, is not
// stopped by its own process being alive.
func TestTheHeartbeatIsNotRefusedByARunningHeartbeat(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	beats := apptest.NewFakeHeartbeats()
	beats.Start("mw-gq6.1", 4242)
	runner := apptest.NewFakeRunner()
	if err := runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName("mw-gq6.1"), Command: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	ticks := 0
	next := application.Next{
		Tracker: tracker, Runner: runner, Heartbeats: beats,
		HeartbeatWait: func(context.Context, time.Duration) error {
			if ticks++; ticks > 1 {
				return context.Canceled
			}
			return nil
		},
	}
	if err := next.Heartbeat(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatalf("the heartbeat returned %v", err)
	}
	if ticks < 2 {
		t.Errorf("expected the heartbeat to wait and tick, ticked %d times", ticks)
	}
}
