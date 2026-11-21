package application_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-gq6.1263: lampas batch 524's fixer ran 5 minutes, and its second gate began
// about 50 minutes later. The batch shares the rig's merge slot while it waits for
// its fixer, so a Builder's check runs meanwhile, and takes the slot alone again
// once that check has ended. But the re-gate then waited for a busy host with the
// slot shared once more, which let a new check in, and the batch waited for that
// one too before its tests. Once the batch has taken the slot back after its
// fixer it goes ahead of new checks: the re-gate waits at most for the one check
// that was running when the fixer ended.

// aSlotOfChecks is the rig's merge slot with the Builders' checks of the rig at
// it: a check starts beside the batch while the batch has yielded the slot, and
// waits behind it while the batch holds it alone. Taking the slot alone again
// waits for every check running beside it.
type aSlotOfChecks struct {
	mu    sync.Mutex
	state string // "alone" or "shared"
	// running is how many checks run beside the yielded slot.
	running int
	// waitedFor is, for each time the slot was taken alone again, how many running
	// checks that waited for.
	waitedFor []int
}

func (s *aSlotOfChecks) Release(context.Context) error { return nil }
func (s *aSlotOfChecks) HeldBy() string                { return "mw land on laptop, landing a batch of millwright" }

func (s *aSlotOfChecks) Yield(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == "alone" {
		s.state = "shared"
	}
	return nil
}

func (s *aSlotOfChecks) Resume(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != "shared" {
		return nil
	}
	s.waitedFor = append(s.waitedFor, s.running)
	s.running, s.state = 0, "alone"
	return nil
}

func (s *aSlotOfChecks) SharedByOthers() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == "shared" && s.running > 0
}

// aCheckArrives is a Builder's `mw check --part N` of the rig: it starts when the
// slot is yielded, and waits behind the batch when it is not.
func (s *aSlotOfChecks) aCheckArrives() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == "shared" {
		s.running++
	}
}

func (s *aSlotOfChecks) waited() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.waitedFor...)
}

// aFixerAChecksStartsBeside is the batch's fixer: while the batch waits for it a
// check of the rig arrives, and the fixer ends having done its work.
type aFixerAChecksStartsBeside struct {
	*apptest.FakeRunner
	slot *aSlotOfChecks
}

func (r aFixerAChecksStartsBeside) Wait(ctx context.Context, name string) (application.SessionStatus, error) {
	r.slot.aCheckArrives()
	r.FakeRunner.Exit(name, 0)
	return r.FakeRunner.Wait(ctx, name)
}

func TestABatchResumesTheSlotAheadOfNewChecks(t *testing.T) {
	slot := &aSlotOfChecks{state: "alone"}
	runner := aFixerAChecksStartsBeside{FakeRunner: apptest.NewFakeRunner(), slot: slot}
	vault := newFakeVault()
	vault.written[application.BatchFixerName("millwright", 524)+"/"+application.ResultFileName] = `{"subtype":"success"}`
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: vault, Landing: &fakeRetryLanding{}, Runner: runner, Checks: &scriptedChecks{passes: []bool{true}},
		Boot: application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
		Host: "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
	}, Fix: true}.HoldingTheSlot(slot)
	ctx := application.WithQueuePrecedence(context.Background())
	dir := "/rigs/millwright/.landing-524"
	report := &application.BatchReport{Rig: "millwright", Target: "main", Number: 524, Base: "base0123", Tested: true}

	land.FixTheBatch(ctx, dir, report)
	if !report.Fixer.Started {
		t.Fatalf("expected the fixer to be started, got %+v (held %q)", report.Fixer, report.Held)
	}
	if got := slot.waited(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected the batch to take the slot alone again once, after the check that ran beside its fixer, waits %v", got)
	}

	// The re-gate finds the host busy, still at the check's load, and waits for it
	// to calm; a new check of the rig arrives meanwhile.
	land.Next.Load = &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}}
	land.Next.LoadWait = func(context.Context, time.Duration) error {
		slot.aCheckArrives()
		return nil
	}
	gate := land.GateTheBatch(ctx, dir, report)
	if !gate.Passed {
		t.Fatalf("expected the re-gate to pass, got %+v", gate)
	}
	waits := 0
	for _, n := range slot.waited() {
		waits += n
	}
	if waits > 1 {
		t.Fatalf("after its fixer the batch waited for %d running checks before its re-gate's tests (waits %v): a check that arrived during the re-gate went ahead of it, want at most the one running when the fixer ended", waits, slot.waited())
	}
}
