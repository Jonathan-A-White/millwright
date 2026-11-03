package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aWholeCheckBesideTheHostsGateLock is mw check with no --part on a rig whose
// gate is not split, with this host's gate lock a close-out's gate may hold
// (mw-gq6.906).
type aWholeCheckBesideTheHostsGateLock struct {
	gate    *hostlock.Gate
	slots   *rig.Slots
	rigDir  string
	checks  application.Checks
	records *mapRecords
	told    *lockedBuffer
}

func newWholeCheckBesideTheGateLock(t *testing.T, checks application.Checks) *aWholeCheckBesideTheHostsGateLock {
	t.Helper()
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &aWholeCheckBesideTheHostsGateLock{
		gate:    hostlock.NewGate(t.TempDir(), hostlock.WithGatePoll(5*time.Millisecond)),
		slots:   rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond)),
		rigDir:  rigDir,
		checks:  checks,
		records: &mapRecords{},
		told:    &lockedBuffer{},
	}
}

func (w *aWholeCheckBesideTheHostsGateLock) check(wait time.Duration) (application.CheckReport, error) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	return application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: wait,
		GateNoticeEvery: 40 * time.Millisecond,
		Host:            "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
}

type writerOf2 struct{ b *lockedBuffer }

func (w writerOf2) Write(p []byte) (int, error) { w.b.WriteString(string(p)); return len(p), nil }

func writerOf(b *lockedBuffer) io.Writer { return writerOf2{b} }

// holdingChecks is the rig's tests, which stay running until released.
type holdingChecks struct {
	started chan struct{}
	release chan struct{}
}

func (h *holdingChecks) Run(context.Context, string, string) (application.Checked, error) {
	close(h.started)
	<-h.release
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

func TestACloseOutsGateHoldingTheHostsGateLockMakesAWholeCheckWaitAndEndInOneLineNamingTheHolder(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOut.Holding.Release(context.Background())

	started := time.Now()
	report, err := w.check(150 * time.Millisecond)
	if err == nil {
		t.Fatalf("expected the check not to pass while a close-out's gate held the host's gate lock\n%s", report)
	}
	if time.Since(started) > 4*time.Second {
		t.Errorf("expected the check to give up at its wait, it took %s", time.Since(started))
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the tests were not run, got %+v", report.Refusals)
	}
	for _, want := range []string{"did not start", "gate lock", "mw-gq6.894", "since ", "nothing was run", "`mw check mw-x.1`"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("expected the line to say %q, got %q", want, refusal.Why)
		}
	}
	if strings.Contains(refusal.Why, "\n") {
		t.Errorf("expected one line, got %q", refusal.Why)
	}
	if checks.runs != 0 {
		t.Errorf("expected the tests not to have run, they ran %d time(s)", checks.runs)
	}
	// The wait said, while it went on, who held the lock.
	if !strings.Contains(w.told.String(), "mw-gq6.894") || !strings.Contains(w.told.String(), "still waiting") {
		t.Errorf("expected the wait to say again who held the gate lock, got %q", w.told.String())
	}
	// The merge slot was given back with the wait.
	if again, err := w.slots.Take(context.Background(), w.rigDir, "a landing"); err != nil {
		t.Errorf("expected the merge slot to be free after the check gave up: %v", err)
	} else {
		again.Release(context.Background())
	}
}

func TestACheckWaitingForTheHostsGateLockRunsOnceItIsGivenBack(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		closeOut.Holding.Release(context.Background())
	}()
	report, err := w.check(5 * time.Second)
	if err != nil {
		t.Fatalf("expected the check to pass once the gate lock was free, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run once, they ran %d time(s)", checks.runs)
	}
	if !strings.Contains(w.told.String(), "gate lock is held by mw-gq6.894") {
		t.Errorf("expected the check to say it waited for the gate lock, got %q", w.told.String())
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the gate lock to be given back when the check ended, %q still holds it", holder)
	}
}

func TestAWholeCheckHoldingTheHostsGateLockMakesACloseOutsGateWait(t *testing.T) {
	checks := &holdingChecks{started: make(chan struct{}), release: make(chan struct{})}
	w := newWholeCheckBesideTheGateLock(t, checks)
	done := make(chan error, 1)
	go func() {
		_, err := w.check(5 * time.Second)
		done <- err
	}()
	select {
	case <-checks.started:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the check to start the rig's tests")
	}

	var heldBy string
	waitCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := w.gate.Take(waitCtx, "mw-gq6.894", func(by string) { heldBy = by }); err == nil {
		t.Fatal("expected a close-out's gate to wait while a check held the host's gate lock")
	}
	if !strings.Contains(heldBy, "mw-x.1") || !strings.Contains(heldBy, "check") {
		t.Errorf("expected the close-out to be told a check of mw-x.1 holds the lock, got %q", heldBy)
	}

	close(checks.release)
	if err := <-done; err != nil {
		t.Fatalf("expected the check to pass, got %v", err)
	}
	turn, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatalf("expected the gate lock to be free once the check ended: %v", err)
	}
	turn.Holding.Release(context.Background())
}

func TestAWholeCheckThatFailsGivesTheHostsGateLockBack(t *testing.T) {
	w := newWholeCheckBesideTheGateLock(t, &scriptedChecks{passes: []bool{false}})
	if _, err := w.check(time.Second); err == nil {
		t.Fatal("expected the check to fail")
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the gate lock to be free after a failed check, %q holds it", holder)
	}
}

func TestACheckHandedBackOnTheHostsGateLockTwiceInOneAttemptCountsTheSecondOnceAndSaysSo(t *testing.T) {
	w := newWholeCheckBesideTheGateLock(t, &scriptedChecks{passes: []bool{true}})
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOut.Holding.Release(context.Background())

	first, _ := w.check(80 * time.Millisecond)
	one := slotRefusalOf(first)
	if one == nil || strings.Contains(one.Why, "counted once") {
		t.Fatalf("expected the first hand-back to be a plain wait, got %+v", first.Refusals)
	}
	second, _ := w.check(80 * time.Millisecond)
	two := slotRefusalOf(second)
	if two == nil {
		t.Fatalf("expected a refusal on the second hand-back, got %+v", second.Refusals)
	}
	for _, want := range []string{"counted once for this attempt", "not a failure", "`mw check mw-x.1`", "comment on the story"} {
		if !strings.Contains(two.Why, want) {
			t.Errorf("expected the repeat to say %q, got %q", want, two.Why)
		}
	}
}

func TestAWholeCheckWithNoHostGateLockRunsAsBefore(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: w.slots, Records: w.records, Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	if err != nil || checks.runs != 1 {
		t.Fatalf("expected a check with no gate lock to pass and run the tests once, got %v, %d runs\n%s", err, checks.runs, report)
	}
}

// specsSeeingTheLock is the rig's changed-specs command, which notes whether
// the host's gate lock was held while it ran.
type specsSeeingTheLock struct {
	gate *hostlock.Gate
	held string
}

func (s *specsSeeingTheLock) RunSpecs(context.Context, string, string, string) (application.SpecsChecked, error) {
	s.held = s.gate.Holder()
	return application.SpecsChecked{Checked: application.Checked{Command: "make specs", Passed: true}, Specs: []string{"a.feature"}}, nil
}

func TestAWholeChecksChangedSpecsRunHoldingTheHostsGateLockToo(t *testing.T) {
	w := newWholeCheckBesideTheGateLock(t, &scriptedChecks{passes: []bool{true}})
	specs := &specsSeeingTheLock{gate: w.gate}
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks, SpecChecks: specs,
		Slot: w.slots, GateLock: w.gate, Records: w.records, Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	if err != nil {
		t.Fatalf("expected the check to pass, got %v\n%s", err, report)
	}
	if !strings.Contains(specs.held, "mw-x.1") {
		t.Errorf("expected the changed specs to run holding the gate lock, it was held by %q", specs.held)
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the lock to be given back, %q holds it", holder)
	}
}
