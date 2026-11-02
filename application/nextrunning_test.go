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
	"github.com/Jonathan-A-White/millwright/domain"
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

// mw-gq6.500: the pane line of a session launched before mw-gq6.487 kills the
// heartbeat and starts the close-out at once, and mw ends on TERM by cancelling
// what it was doing, not at once: the desktop's mw-5r3p30.324 was refused as
// "still running (mw next --heartbeat ... is alive as process 1282067)" by its
// own close-out and left no trace but the pane. A heartbeat that goes within
// the grace is a session over, not one still at work.
func TestACloseOutWaitsForTheHeartbeatItsPaneJustKilled(t *testing.T) {
	marks := apptest.NewFakeCloseOutMarks()
	beats := apptest.NewFakeHeartbeats()
	beats.Start("mw-gq6.1", 1282067)
	ticks := 0

	_, _ = application.Next{
		Tracker: apptest.NewFakeTracker(), Heartbeats: beats, CloseOuts: marks,
		HeartbeatGrace: application.DefaultHeartbeatGrace,
		HeartbeatWait: func(context.Context, time.Duration) error {
			if ticks++; ticks == 2 {
				beats.Stop("mw-gq6.1")
			}
			return nil
		},
	}.Run(context.Background(), "mw-gq6.1")

	if len(marks.Writes) == 0 {
		t.Errorf("expected the close-out to go on once the heartbeat went, and it never began (waited %d times)", ticks)
	}
}

// A heartbeat that outlasts the grace is a session at work, and is refused as
// before, after waiting no longer than the grace.
func TestACloseOutStopsWaitingForAHeartbeatThatStaysAlive(t *testing.T) {
	marks := apptest.NewFakeCloseOutMarks()
	beats := apptest.NewFakeHeartbeats()
	beats.Start("mw-gq6.1", 4242)
	var waited time.Duration

	_, err := application.Next{
		Tracker: apptest.NewFakeTracker(), Heartbeats: beats, CloseOuts: marks,
		HeartbeatGrace: application.DefaultHeartbeatGrace,
		HeartbeatWait:  func(_ context.Context, d time.Duration) error { waited += d; return nil },
	}.Run(context.Background(), "mw-gq6.1")

	if err == nil || !strings.Contains(err.Error(), "still running") || !strings.Contains(err.Error(), "4242") {
		t.Errorf("expected the refusal naming the running heartbeat, got %v", err)
	}
	if waited == 0 || waited > application.DefaultHeartbeatGrace {
		t.Errorf("expected a wait of up to %s, waited %s", application.DefaultHeartbeatGrace, waited)
	}
	if len(marks.Writes) != 0 {
		t.Errorf("expected no close-out mark, got %+v", marks.Writes)
	}
}

// mw-gq6.820: a heartbeat that fails the same way every tick says so when it
// first fails, and its repeats once, when the session is over.
func TestTheHeartbeatSaysARepeatedFailureOnceAndItsRepeatsOnce(t *testing.T) {
	tracker := apptest.NewFakeTracker() // no such story in progress: every renewal is refused alike
	runner := apptest.NewFakeRunner()
	if err := runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName("mw-gq6.1"), Command: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	ticks := 0
	var out strings.Builder
	next := application.Next{
		Tracker: tracker, Runner: runner, Out: &out,
		HeartbeatWait: func(context.Context, time.Duration) error {
			if ticks++; ticks > 5 {
				return context.Canceled
			}
			return nil
		},
	}
	if err := next.Heartbeat(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatalf("the heartbeat returned %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected the failure once and its repeats once, got %d lines:\n%s", len(lines), out.String())
	}
	if strings.Contains(lines[0], "times") || !strings.Contains(lines[1], "the same way 5 times from") {
		t.Errorf("expected the first failure plain and then the repeats counted, got:\n%s", out.String())
	}
}

// scriptedStatus is a runner whose Status answers from a script: an error for
// each of the first slow looks, then running for some looks, then gone.
type scriptedStatus struct {
	*apptest.FakeRunner
	slow    int
	running int
	looks   int
}

func (s *scriptedStatus) Status(ctx context.Context, name string) (application.SessionStatus, error) {
	s.looks++
	switch {
	case s.looks <= s.slow:
		return application.SessionStatus{}, errors.New("tmux list-panes -t =" + name + ": did not finish in 5s and was stopped: context deadline exceeded")
	case s.looks <= s.slow+s.running:
		return application.SessionStatus{Name: name, State: application.StateRunning}, nil
	}
	return application.SessionStatus{Name: name, State: application.StateGone}, nil
}

// mw-gq6.885: a session tmux is too slow to answer about is not known to be
// over, so its lease is renewed on that tick, the failure is told once with its
// count, and the loop ends only when the session is gone.
func TestTheHeartbeatGoesOnWhenTheSessionCannotBeAskedAbout(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	if err := tracker.ClaimStory(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	runner := &scriptedStatus{FakeRunner: apptest.NewFakeRunner(), slow: 3, running: 2}
	ticks := 0
	var out strings.Builder
	next := application.Next{
		Tracker: &beatCounter{WorkTracker: tracker}, Runner: runner, Out: &out,
		HeartbeatWait: func(context.Context, time.Duration) error { ticks++; return nil },
	}
	counter := next.Tracker.(*beatCounter)
	if err := next.Heartbeat(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatalf("the heartbeat returned %v, and a session that could not be asked about is not an error", err)
	}
	if runner.looks != 6 || ticks != 6 {
		t.Errorf("expected 6 looks (3 slow, 2 running, 1 gone) in 6 ticks, got %d looks in %d ticks", runner.looks, ticks)
	}
	if counter.beats != 5 {
		t.Errorf("expected the claim renewed on each of the 5 ticks before the session was gone, renewed %d times", counter.beats)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected the failed look once and its repeats once, got %d lines:\n%s", len(lines), out.String())
	}
	if strings.Contains(lines[0], "times") || !strings.Contains(lines[1], "the same way 3 times from") {
		t.Errorf("expected the first failure plain and then the 3 counted, got:\n%s", out.String())
	}
}

// A look that fails when the loop is being ended is the end, not a failure to
// tell, and a session that is gone ends the loop on the very first look.
func TestTheHeartbeatEndsOnGoneAndOnCancelWithoutNoteOrRenewal(t *testing.T) {
	tracker := &beatCounter{WorkTracker: apptest.NewFakeTracker()}
	var out strings.Builder
	gone := application.Next{Tracker: tracker, Runner: &scriptedStatus{FakeRunner: apptest.NewFakeRunner()}, Out: &out,
		HeartbeatWait: func(context.Context, time.Duration) error { return nil }}
	if err := gone.Heartbeat(context.Background(), "mw-gq6.1"); err != nil || tracker.beats != 0 || out.Len() != 0 {
		t.Errorf("expected a gone session to end it quietly, got err %v, %d renewals, %q", err, tracker.beats, out.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelling := application.Next{Tracker: tracker, Runner: &cancelsOnLook{FakeRunner: apptest.NewFakeRunner(), cancel: cancel}, Out: &out,
		HeartbeatWait: func(context.Context, time.Duration) error { return nil }}
	if err := cancelling.Heartbeat(ctx, "mw-gq6.1"); err != nil || tracker.beats != 0 || out.Len() != 0 {
		t.Errorf("expected a cancelled loop to end it quietly, got err %v, %d renewals, %q", err, tracker.beats, out.String())
	}
}

// cancelsOnLook cancels its context and fails the look, as a stopped mw next does.
type cancelsOnLook struct {
	*apptest.FakeRunner
	cancel context.CancelFunc
}

func (c *cancelsOnLook) Status(context.Context, string) (application.SessionStatus, error) {
	c.cancel()
	return application.SessionStatus{}, context.Canceled
}

// beatCounter counts the renewals made through it.
type beatCounter struct {
	application.WorkTracker
	beats int
}

func (b *beatCounter) HeartbeatClaim(ctx context.Context, id string) error {
	b.beats++
	return b.WorkTracker.HeartbeatClaim(ctx, id)
}
