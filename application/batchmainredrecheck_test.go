package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1706: lampas batch 676 (green, unrelated) moved main, and the follower
// gated batch 677 for ~30 minutes though the test the last hold named still failed
// on main. A rig whose last batch was held on main's red runs that hold's tests
// by name on the new main alone first: still red, it holds at once; else it gates.

const mainRedCarrier = "mw-e.99"

// aMainRedRecheck is a batch of two on rig millwright whose last line is a hold
// on main's red at head "oldhead", carried by mainRedCarrier, naming TestA and
// TestB; the batch's base is "base0123", where main has moved to.
type aMainRedRecheck struct {
	aSetAsideBatch
	named   *namedTestsThat
	metrics *apptest.FakeBatchMetrics
}

func newAMainRedRecheck(t *testing.T, onMain func(context.Context, []string) (application.NamedRun, error)) *aMainRedRecheck {
	t.Helper()
	a := &aMainRedRecheck{aSetAsideBatch: aBatchOf(t, 2), metrics: &apptest.FakeBatchMetrics{}}
	a.named = &namedTestsThat{run: onMain}
	a.tracker.AddStory("mw-e", domain.Story{ID: mainRedCarrier, Title: "[bug] main is red: TestA, TestB"})
	a.land.Fix = false
	a.land.Metrics = a.metrics
	a.land.Next.NamedChecks = a.named
	a.land.Next.Checks = a.checks
	a.land.Next.Slot = aSlot{}
	a.land.Next.Rigs = map[string]string{"millwright": "/rigs/millwright"}
	a.land.Next.MainAloneBound = 10 * time.Minute
	a.land.Next.LoadWait = func(context.Context, time.Duration) error { return nil }
	a.land.Next.Mailbox = apptest.NewFakeMailbox()
	a.land.Next.Out, a.land.Next.Err = io.Discard, io.Discard
	held := application.BatchMetric{Kind: application.BatchKind, At: time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC),
		Rig: "millwright", Host: "laptop", Batch: 594, Stories: 2, IDs: []string{"mw-e.1", "mw-e.2"}, Outcome: application.BatchHeld,
		Red: true, MainRed: true, MainHead: "oldhead", MainBranch: "main", MainTests: []string{"TestA", "TestB"}, MainBug: mainRedCarrier}
	if err := a.metrics.AppendBatch(context.Background(), held); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestARigHeldOnMainsRedWhoseTestsStillFailOnTheNewMainIsHeldAgainWithoutTheGate(t *testing.T) {
	a := newAMainRedRecheck(t, failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"))
	a.run(t)

	r := a.report
	if r.Landed || !strings.Contains(r.Held, "main is red") || !strings.Contains(r.Held, mainRedCarrier) {
		t.Fatalf("expected the batch held on main's red, carried by %s, got landed %v, held %q", mainRedCarrier, r.Landed, r.Held)
	}
	if a.checks.runs != 0 {
		t.Errorf("expected the batch's gate never run, got %d runs", a.checks.runs)
	}
	if len(a.named.asked) != 1 || strings.Join(a.named.asked[0], ",") != "TestA,TestB" {
		t.Errorf("expected the hold's two tests run on main alone once, got %v", a.named.asked)
	}
	if got := a.landing.pushed; got != 0 {
		t.Errorf("expected nothing pushed, got %d", got)
	}
	// The line the batch leaves says main's red at the new head, with the same
	// tests and carrier, so the rig is not due again until main moves again.
	m := r.Metric(time.Date(2026, 11, 24, 16, 0, 0, 0, time.UTC))
	if !m.MainRed || m.MainHead != "base0123" || m.MainBranch != "main" || m.MainBug != mainRedCarrier || strings.Join(m.MainTests, ",") != "TestA,TestB" {
		t.Errorf("expected the line to hold main's red at base0123 with its tests and carrier, got %+v", m)
	}
	if _, waits := application.HeldOnMainRed([]application.BatchMetric{m}); !waits {
		t.Errorf("expected the new line to make the rig wait on main's head, got %+v", m)
	}
}

func TestARigHeldOnMainsRedWhoseTestsPassOnTheNewMainGatesTheBatchAsBefore(t *testing.T) {
	a := newAMainRedRecheck(t, func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{Checked: application.Checked{Command: "go test -run", Passed: true}, Ran: true}, nil
	})
	a.run(t)

	r := a.report
	if !r.Landed || r.Held != "" {
		t.Fatalf("expected the batch landed, got landed %v, held %q", r.Landed, r.Held)
	}
	if a.checks.runs != 1 {
		t.Errorf("expected the gate run once, got %d runs", a.checks.runs)
	}
	if len(a.named.asked) != 1 {
		t.Errorf("expected the hold's tests run on main alone once, got %v", a.named.asked)
	}
	if m := r.Metric(time.Now()); m.MainRed {
		t.Errorf("expected a landed batch to say nothing of main's red, got %+v", m)
	}
}

// A carrier that is closed counts as main having moved: nothing is run on main
// alone first, and the batch is gated as before.
func TestARigHeldOnMainsRedWhoseCarrierIsClosedGatesTheBatchWithoutTheCheck(t *testing.T) {
	a := newAMainRedRecheck(t, failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"))
	if err := a.tracker.CloseStory(context.Background(), mainRedCarrier, "fixed"); err != nil {
		t.Fatal(err)
	}
	a.run(t)
	if len(a.named.asked) != 0 {
		t.Errorf("expected main not asked while its carrier is closed, got %v", a.named.asked)
	}
	if a.checks.runs != 1 {
		t.Errorf("expected the gate run once, got %d runs", a.checks.runs)
	}
}

// A line that names no tests (one written before the fields, or a host's gap) is
// gated as before.
func TestARigHeldOnMainsRedWithNoTestsKeptIsGatedAsBefore(t *testing.T) {
	a := newAMainRedRecheck(t, failingOnMain("--- FAIL: TestA (0.01s)\nFAIL\n"))
	a.metrics = &apptest.FakeBatchMetrics{}
	a.land.Metrics = a.metrics
	old := application.BatchMetric{Kind: application.BatchKind, Rig: "millwright", Batch: 594, IDs: []string{"mw-e.1"},
		Outcome: application.BatchHeld, Red: true, MainRed: true, MainHead: "oldhead", MainBranch: "main"}
	if err := a.metrics.AppendBatch(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	a.run(t)
	if len(a.named.asked) != 0 || a.checks.runs != 1 {
		t.Errorf("expected the gate run with no check of main first, got %d runs, asked %v", a.checks.runs, a.named.asked)
	}
}
