package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A --part worker is the process a call started for a part that takes the merge
// slot; it outlives the call and waits for the slot itself, for the 40 minutes a
// check not kept to a call waits (mw-gq6.1289). It is not kept to the call, so a
// give-way during its wait is not spent from it: the span the host gave way, read
// from its give-way record, is taken off, and the worker waits out what it had
// left at the freeze (mw-gq6.1479). It is built as mw builds it, with the call's
// budget and place kept that every --part is given.
func TestAPartWorkersSlotWaitDoesNotCountAGiveWay(t *testing.T) {
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	slots := rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond))
	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			landing.Release(context.Background())
		}
	}()
	parts := newWorkerParts(true)
	// A part whose command mw does not know to be worktree-only keeps the slot.
	parts.command = "make screenshots"
	close(parts.release)
	records := &mapRecords{}
	told := &lockedBuffer{}
	clock := newCallClock()
	host := &apptest.FakeGiveWayHost{}
	way := application.GiveWay{Processes: host, Panes: host, Marker: host, Log: host, Host: "laptop", Now: clock.Now}

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
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
			Parts: parts, Records: records, Part: 1, PartWorker: true, Slot: slots,
			PartSlotWait: 40 * time.Minute, KeepPlace: 10 * time.Minute, CallBudget: application.CheckCallBudget,
			GateNoticeEvery: 40 * time.Millisecond, CallNow: clock.Now, GaveWay: way,
			Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": rigDir},
			Out:  writerOf(told),
		}.Run(context.Background(), "mw-x.1")
		done <- result{report, err}
	}()
	// ended fails the test when the worker has ended its wait; it first lets the
	// wait read the clock as it stands now, or end.
	ended := func(when string) {
		t.Helper()
		from := clock.readsOf()
		for deadline := time.Now().Add(hangGuard); clock.readsOf() < from+3; time.Sleep(time.Millisecond) {
			select {
			case got := <-done:
				t.Fatalf("expected the part worker still to wait for the merge slot %s, it ended:\n%s\n%v\nit said:\n%s", when, got.report, got.err, told.String())
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("expected the wait to read the check's clock again")
			}
		}
		select {
		case got := <-done:
			t.Fatalf("expected the part worker still to wait for the merge slot %s, it ended:\n%s\n%v\nit said:\n%s", when, got.report, got.err, told.String())
		default:
		}
	}

	// Seven minutes of the worker's wait are spent; 33 are left when the host gives way.
	clock.readAgain(t, 3)
	clock.Pass(7 * time.Minute)
	clock.readAgain(t, 3)
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	// The host gives way for 45 minutes, longer than the 33 the wait had left.
	clock.Pass(45 * time.Minute)
	if err := way.Off(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	ended("after the host went on")
	// 32 of the 33 minutes it had at the freeze are spent after it: it still waits.
	clock.Pass(32 * time.Minute)
	ended("with a minute of what it had at the freeze left")
	if parts.ranCount() != 0 {
		t.Fatalf("expected the part not to run while the slot was held, it ran %d time(s)", parts.ranCount())
	}

	landing.Release(context.Background())
	released = true
	var got result
	select {
	case got = <-done:
	case <-time.After(hangGuard):
		t.Fatal("expected the part worker to take the merge slot once it was given back")
	}
	if got.err != nil {
		t.Fatalf("expected the part to run and pass, got %v\nit said:\n%s", got.err, told.String())
	}
	if parts.ranCount() != 1 {
		t.Errorf("expected the part to have run once, it ran %d time(s)", parts.ranCount())
	}
	text, err := records.ReadRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 1))
	if err != nil || !strings.Contains(text, "passed") {
		t.Errorf("expected the part's record to say it passed, got %q, %v", text, err)
	}
}
