package steps

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// endedRun is a run of mw talk wait that ended after `after` from the event.
func endedRun(event time.Time, armedBefore, after time.Duration) *talkWaitRun {
	run := &talkWaitRun{done: make(chan struct{}), armedAt: event.Add(-armedBefore), endedAt: event.Add(after)}
	close(run.done)
	return run
}

// A wait that ended in time is not failed because the scenario looked late: the
// bound is judged on when the wait ended. The old step raced a closed done
// channel against an already-fired timer and lost half the time.
func TestEndsWithinJudgesWhenTheWaitEndedNotWhenTheScenarioLooked(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	for i := 0; i < 200; i++ {
		c := &talkWaitContext{lastEvent: event, run: endedRun(event, 0, 50*time.Millisecond)}
		if err := c.endsWithin(1); err != nil {
			t.Fatalf("run %d: a wait that ended 50 ms after the event failed a 1 s bound: %v", i, err)
		}
	}
}

func TestEndsWithinFailsAWaitThatEndedLate(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	c := &talkWaitContext{lastEvent: event, run: endedRun(event, 0, time.Second+talkWaitLoadSlack+time.Second)}
	err := c.endsWithin(1)
	if err == nil || !strings.Contains(err.Error(), "after the event") {
		t.Fatalf("a wait that ended past the bound and the host's slack passed a 1 s bound: %v", err)
	}
}

// A host under load starves the stream's goroutines for a second or more
// between the event and the wait hearing it, while the wait's own work, the
// index-to-print time it reports, stays a millisecond (a gate run at load 10
// of 16 ended a wait 1.362 s after the event with 1 ms of its own). The bound
// is held to the wait's own time, and the wall clock gets the host's slack.
func TestEndsWithinPassesAWaitWhoseOwnWorkWasQuickOnAStarvedHost(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	run := endedRun(event, 0, 1362*time.Millisecond)
	run.report.IndexToPrint = time.Millisecond
	c := &talkWaitContext{lastEvent: event, run: run}
	if err := c.endsWithin(1); err != nil {
		t.Fatalf("a wait that took 1 ms itself failed because the host delayed the event 1.4 s: %v", err)
	}
}

func TestEndsWithinFailsAWaitWhoseOwnWorkWasSlow(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	run := endedRun(event, 0, 1500*time.Millisecond)
	run.report.IndexToPrint = 1400 * time.Millisecond
	c := &talkWaitContext{lastEvent: event, run: run}
	err := c.endsWithin(1)
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("a wait that spent 1.4 s of its own passed a 1 s bound: %v", err)
	}
}

// The time the ideas took to be kept, from arming to the event, is not counted
// against the wait ending on the event.
func TestEndsWithinStartsAtTheEventNotAtTheArming(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	c := &talkWaitContext{lastEvent: event, run: endedRun(event, talkWaitLoadSlack+1500*time.Millisecond, 500*time.Millisecond)}
	if err := c.endsWithin(1); err != nil {
		t.Fatalf("6.5 s of keeping ideas before the event was counted against the wait: %v", err)
	}
	// With the arming as its start the same run is 7 s long and fails.
	c.lastEvent = c.run.armedAt
	if err := c.endsWithin(1); err == nil {
		t.Fatal("a wait that ended 7 s after the bound's start passed a 1 s bound")
	}
}

// The scenario about the ideas' time judges that the wait ended on the event, not
// how long after it: a host at load 38 of 16 ended a wait 12 s after the event
// (mw-gq6.913), and the wait was still right.
func TestEndsOnTheEventPassesAWaitThatEndedLateOnALoadedHost(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	c := &talkWaitContext{lastEvent: event, run: endedRun(event, 4*time.Second, 12*time.Second)}
	if err := c.endsOnTheEvent(); err != nil {
		t.Fatalf("a wait that ended 12 s after the event, on a loaded host, failed: %v", err)
	}
}

// A wait that counts the ideas' time ends on that time, before the event comes.
func TestEndsOnTheEventFailsAWaitThatEndedBeforeTheEvent(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	c := &talkWaitContext{lastEvent: event, run: endedRun(event, 4*time.Second, -time.Second)}
	err := c.endsOnTheEvent()
	if err == nil || !strings.Contains(err.Error(), "before the event") {
		t.Fatalf("a wait that ended 1 s before the event passed: %v", err)
	}
}

func TestEndsOnTheEventFailsAWaitThatFailed(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	run := endedRun(event, 0, time.Second)
	run.err = errors.New("stream gone")
	c := &talkWaitContext{lastEvent: event, run: run}
	if err := c.endsOnTheEvent(); err == nil {
		t.Fatal("a wait that failed passed")
	}
}
