package steps

import (
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
	c := &talkWaitContext{lastEvent: event, run: endedRun(event, 0, 3*time.Second)}
	err := c.endsWithin(1)
	if err == nil || !strings.Contains(err.Error(), "after the event") {
		t.Fatalf("a wait that ended 3 s after the event passed a 1 s bound: %v", err)
	}
}

// The time the ideas took to be kept, from arming to the event, is not counted
// against the wait ending on the event.
func TestEndsWithinStartsAtTheEventNotAtTheArming(t *testing.T) {
	event := time.Now().Add(-time.Minute)
	c := &talkWaitContext{lastEvent: event, run: endedRun(event, 1500*time.Millisecond, 500*time.Millisecond)}
	if err := c.endsWithin(1); err != nil {
		t.Fatalf("1.5 s of keeping ideas before the event was counted against the wait: %v", err)
	}
	// With the arming as its start the same run is 2 s long and fails.
	c.lastEvent = c.run.armedAt
	if err := c.endsWithin(1); err == nil {
		t.Fatal("a wait that ended 2 s after the bound's start passed a 1 s bound")
	}
}
