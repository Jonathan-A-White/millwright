package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// readsOf is how many times the call's clock has been read.
func (c *callClock) readsOf() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// readAgain waits until the clock has been read n more times: the bound of a wait
// reads it on every poll, so the wait has looked at the clock as it stands now.
func (c *callClock) readAgain(t *testing.T, n int) {
	t.Helper()
	from := c.readsOf()
	for deadline := time.Now().Add(hangGuard); c.readsOf() < from+n; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("expected the wait to read the check's clock again")
		}
	}
}

// A check waiting for the gate lock whose host gives way for longer than the wait
// has left, and then goes on, still has the limit it had at the freeze: the span
// the host gave way, read from its give-way record, is not spent (mw-gq6.1458).
// Seen on the desktop: a gate worker with 40m began at 21:52:18Z, the host gave way
// from 21:59:33Z to 22:30:24Z, and the check woke with 57s left, lost the lock to
// the story queued behind it and ended tests-not-run with its wait limit reached.
func TestAGiveWayDoesNotSpendACheckWaitForTheGateLock(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	holder, err := w.gate.Take(context.Background(), "mw-n9ad55d.5", nil)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			holder.Holding.Release(context.Background())
		}
	}()
	host := &apptest.FakeGiveWayHost{}
	way := application.GiveWay{Processes: host, Panes: host, Marker: host, Log: host, Host: "laptop", Now: w.clock.Now}

	type result struct {
		report application.CheckReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		tracker := apptest.NewFakeTracker()
		tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
		tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
		report, err := application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
			Slot: w.slots, GateLock: w.gate, Records: w.records,
			PartSlotWait: 40 * time.Minute, HandBackAtOnce: true,
			GateNoticeEvery: 40 * time.Millisecond, CallNow: w.clock.Now, GaveWay: way,
			Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": w.rigDir},
			Out:  writerOf(w.told),
		}.Run(context.Background(), "mw-x.1")
		done <- result{report, err}
	}()
	for deadline := time.Now().Add(hangGuard); w.checkSaidItWaits() < 1; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("expected the check to wait for the gate lock, it said:\n%s", w.told.String())
		}
	}

	// Seven minutes of the wait are spent; 33 are left when the host gives way.
	w.clock.Pass(7 * time.Minute)
	w.clock.readAgain(t, 3)
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	// The host gives way for 45 minutes, longer than the 33 the wait had left.
	w.clock.Pass(45 * time.Minute)
	if err := way.Off(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	w.clock.readAgain(t, 3)
	select {
	case got := <-done:
		t.Fatalf("expected the check still to wait after the host went on, it ended:\n%s\n%v\nit said:\n%s", got.report, got.err, w.told.String())
	default:
	}
	// 32 of the 33 minutes it had at the freeze are spent after it: it still waits.
	w.clock.Pass(32 * time.Minute)
	w.clock.readAgain(t, 3)
	select {
	case got := <-done:
		t.Fatalf("expected the check to wait out the 33 minutes it had at the freeze, it ended:\n%s\n%v\nit said:\n%s", got.report, got.err, w.told.String())
	default:
	}

	holder.Holding.Release(context.Background())
	released = true
	var got result
	select {
	case got = <-done:
	case <-time.After(hangGuard):
		t.Fatal("expected the check to take the gate lock once it was given back")
	}
	if got.err != nil || !got.report.Passed() {
		t.Fatalf("expected the check to run the tests and pass, got %v\n%s\nit said:\n%s", got.err, got.report, w.told.String())
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to have run once, they ran %d time(s)", checks.runs)
	}
	if strings.Contains(w.told.String(), "wait limit") {
		t.Errorf("expected no word of the wait limit, the check said:\n%s", w.told.String())
	}
}

// The span a host gave way is read from its give-way record: each off line's
// gave_way_ms, ending where the host went on, and the marker while it still
// stands; only the part of each that falls inside the times asked is counted.
func TestTheSpanAHostGaveWayIsReadFromItsGiveWayRecord(t *testing.T) {
	clock := newCallClock()
	at := clock.Now()
	host := &apptest.FakeGiveWayHost{}
	way := application.GiveWay{Processes: host, Panes: host, Marker: host, Log: host, Host: "desktop", Now: clock.Now}
	ctx := context.Background()

	clock.Pass(10 * time.Minute)
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	clock.Pass(31 * time.Minute)
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	clock.Pass(5 * time.Minute)
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	clock.Pass(4 * time.Minute)
	now := clock.Now()

	for _, c := range []struct {
		name     string
		from, to time.Time
		want     time.Duration
	}{
		{"both spans, the second still standing", at, now, 35 * time.Minute},
		{"the first span alone", at, at.Add(45 * time.Minute), 31 * time.Minute},
		{"half of the first span", at.Add(25*time.Minute + 30*time.Second), at.Add(45 * time.Minute), 15*time.Minute + 30*time.Second},
		{"before either", at, at.Add(10 * time.Minute), 0},
		{"between them", at.Add(41 * time.Minute), at.Add(46 * time.Minute), 0},
	} {
		if got := way.GaveWayBetween(ctx, c.from, c.to); got != c.want {
			t.Errorf("%s: expected %s, got %s", c.name, c.want, got)
		}
	}
}

// mw giveway status names how long the host last gave way, from the off line.
func TestGiveWayStatusNamesHowLongTheHostLastGaveWay(t *testing.T) {
	clock := newCallClock()
	host := &apptest.FakeGiveWayHost{}
	way := application.GiveWay{Processes: host, Panes: host, Marker: host, Log: host, Host: "desktop", Now: clock.Now}
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	clock.Pass(31 * time.Minute)
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	told := &lockedBuffer{}
	way.Out = writerOf(told)
	if err := way.Status(ctx); err != nil {
		t.Fatal(err)
	}
	want := "last gave way for 31m0s, from 2026-11-05T00:00:00Z to 2026-11-05T00:31:00Z"
	if !strings.Contains(told.String(), want) {
		t.Errorf("expected status to say %q, it said:\n%s", want, told.String())
	}
}

// A check kept to a Builder's call counts the span the host gave way all the same:
// the harness's own clock, which cuts the call off, went on through it, so a wait
// that outlived it would be cut off with nothing said.
func TestACheckKeptToABuildersCallCountsAGiveWay(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	holder, err := w.gate.Take(context.Background(), "mw-n9ad55d.5", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Holding.Release(context.Background())
	host := &apptest.FakeGiveWayHost{}
	way := application.GiveWay{Processes: host, Panes: host, Marker: host, Log: host, Host: "laptop", Now: w.clock.Now}

	done := make(chan application.CheckReport, 1)
	go func() {
		tracker := apptest.NewFakeTracker()
		tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
		tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
		report, _ := application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
			Slot: w.slots, GateLock: w.gate, Records: w.records,
			PartSlotWait: 9 * time.Minute, CallBudget: 10 * time.Minute,
			GateNoticeEvery: 40 * time.Millisecond, CallNow: w.clock.Now, GaveWay: way,
			Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": w.rigDir},
			Out:  writerOf(w.told),
		}.Run(context.Background(), "mw-x.1")
		done <- report
	}()
	for deadline := time.Now().Add(hangGuard); w.checkSaidItWaits() < 1; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("expected the check to wait for the gate lock, it said:\n%s", w.told.String())
		}
	}
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	w.clock.Pass(31 * time.Minute)
	if err := way.Off(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case report := <-done:
		if checks.runs != 0 {
			t.Errorf("expected the tests not to have run, they ran %d time(s)", checks.runs)
		}
		if refusal := slotRefusalOf(report); refusal == nil {
			t.Errorf("expected a refusal that the tests were not run, got %+v", report.Refusals)
		}
	case <-time.After(hangGuard):
		t.Fatal("expected the check kept to a call to end its wait once the call's clock passed it")
	}
}
