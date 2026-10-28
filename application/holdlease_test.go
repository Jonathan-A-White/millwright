package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// refusingTracker is a tracker whose heartbeats all fail with err; the rest of
// WorkTracker is never reached by holdLease.
type refusingTracker struct {
	WorkTracker
	mu    sync.Mutex
	err   error
	beats int
}

func (r *refusingTracker) HeartbeatClaim(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beats++
	return r.err
}

func (r *refusingTracker) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beats
}

// leaseClock moves time for holdLease: the first ticks waits return at once,
// each a HeartbeatInterval later by Now, and the wait after them holds until the
// landing is over, after saying it was reached.
func leaseClock(ticks int, start time.Time) (wait func(context.Context, time.Duration) error, now func() time.Time, reached chan struct{}) {
	var mu sync.Mutex
	at := start
	waits := 0
	reached = make(chan struct{})
	wait = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		waits++
		n := waits
		at = at.Add(d)
		mu.Unlock()
		if n > ticks {
			if n == ticks+1 {
				close(reached)
			}
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return at
	}
	return wait, now, reached
}

// mw-gq6.820: one failed renewal repeated every interval of a landing is one
// line, with how many times and since when.
func TestHoldLeaseSaysARepeatedRenewalFailureOnceWithHowManyTimes(t *testing.T) {
	tracker := &refusingTracker{err: errors.New("i/o timeout")}
	wait, now, reached := leaseClock(24, time.Date(2026, 10, 27, 23, 20, 0, 0, time.UTC))
	n := Next{Tracker: tracker, HeartbeatWait: wait, Now: now}

	release := n.holdLease(context.Background(), "mw-gq6.1")
	<-reached
	notes := release()

	if len(notes) != 1 {
		t.Fatalf("expected one line for 24 identical failures, got %d: %q", len(notes), notes)
	}
	for _, want := range []string{"the claim on mw-gq6.1 could not be heartbeated while it was landed", "24 times", "from 23:22 to 00:08", "i/o timeout"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("expected the line to say %q, got %q", want, notes[0])
		}
	}
	if got := tracker.count(); got != 24 {
		t.Errorf("expected every interval to try the renewal again, tried %d times", got)
	}
}

// A single failure, and different failures, stay as they were: one line each.
func TestHoldLeaseKeepsALoneFailureAndDifferentFailuresApart(t *testing.T) {
	tracker := &refusingTracker{err: errors.New("i/o timeout")}
	wait, now, reached := leaseClock(1, time.Date(2026, 10, 27, 23, 20, 0, 0, time.UTC))
	n := Next{Tracker: tracker, HeartbeatWait: wait, Now: now}
	release := n.holdLease(context.Background(), "mw-gq6.1")
	<-reached
	notes := release()
	if len(notes) != 1 || strings.Contains(notes[0], "times") {
		t.Errorf("expected the lone failure as one plain line, got %q", notes)
	}

	flip := &alternatingTracker{}
	wait, now, reached = leaseClock(4, time.Date(2026, 10, 27, 23, 20, 0, 0, time.UTC))
	n = Next{Tracker: flip, HeartbeatWait: wait, Now: now}
	release = n.holdLease(context.Background(), "mw-gq6.1")
	<-reached
	notes = release()
	if len(notes) != 2 || !strings.Contains(notes[0], "2 times") || !strings.Contains(notes[0], "i/o timeout") ||
		!strings.Contains(notes[1], "2 times") || !strings.Contains(notes[1], "connection refused") {
		t.Errorf("expected a line for each of the two failures, each twice, got %q", notes)
	}
}

type alternatingTracker struct {
	WorkTracker
	mu    sync.Mutex
	beats int
}

func (a *alternatingTracker) HeartbeatClaim(context.Context, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.beats++
	if a.beats%2 == 1 {
		return errors.New("i/o timeout")
	}
	return errors.New("connection refused")
}

// mw-gq6.820: a story that is not in progress cannot have its lease renewed at
// all; holdLease says so once and leaves it alone for the rest of the landing.
func TestHoldLeaseStopsRenewingAClaimThatIsNotClaimable(t *testing.T) {
	tracker := &refusingTracker{err: fmt.Errorf("bd heartbeat mw-gq6.1: issue not claimable: mw-gq6.1 has status open")}
	wait, now, reached := leaseClock(24, time.Date(2026, 10, 27, 23, 20, 0, 0, time.UTC))
	n := Next{Tracker: tracker, HeartbeatWait: wait, Now: now}

	release := n.holdLease(context.Background(), "mw-gq6.1")
	// A loop that went on renewing would reach its twenty-fifth wait at once;
	// one that stopped never does, so give it a moment to stop.
	select {
	case <-reached:
	case <-time.After(100 * time.Millisecond):
	}
	notes := release()

	if got := tracker.count(); got != 1 {
		t.Errorf("expected one try and no more after the refusal, tried %d times", got)
	}
	if len(notes) != 1 {
		t.Fatalf("expected one line, got %d: %q", len(notes), notes)
	}
	for _, want := range []string{"mw-gq6.1", "not in progress", "no longer renewed", "not claimable"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("expected the line to say %q, got %q", want, notes[0])
		}
	}
}
