package application_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// The one deadline of a whole mw check covers the waits that come after its
// tests too: the slot and gate lock the changed specs take again, and the second
// wait for a calm host (mw-gq6.918).

// slowChecks is the rig's tests, which take took to run and pass or fail.
type slowChecks struct {
	took time.Duration
	pass bool
	runs int
}

func (s *slowChecks) Run(context.Context, string, string) (application.Checked, error) {
	s.runs++
	time.Sleep(s.took)
	return application.Checked{Command: "make test", Passed: s.pass, Output: "ok\n"}, nil
}

// gateHandedOnAtFirstRelease is the host's gate lock, whose first holder's
// release is followed at once by then, which takes it for another gate.
type gateHandedOnAtFirstRelease struct {
	*hostlock.Gate
	then func()
	once sync.Once

	mu sync.Mutex
	// spentAtTake is, for each Take, whether the wait it was given had already
	// run out: the deadline spent by the tests leaves no time to wait for a lock.
	spentAtTake []bool
}

func (g *gateHandedOnAtFirstRelease) spent() []bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]bool(nil), g.spentAtTake...)
}

func (g *gateHandedOnAtFirstRelease) Take(ctx context.Context, holder string, held func(by string)) (application.GateTurn, error) {
	g.mu.Lock()
	g.spentAtTake = append(g.spentAtTake, ctx.Err() != nil)
	g.mu.Unlock()
	turn, err := g.Gate.Take(ctx, holder, held)
	if err == nil {
		turn.Holding = handingOn{turn.Holding, func() { g.once.Do(g.then) }}
	}
	return turn, err
}

type handingOn struct {
	application.Holding
	then func()
}

func (h handingOn) Release(ctx context.Context) error {
	err := h.Holding.Release(ctx)
	h.then()
	return err
}

// aLandingAtATip is the landing of these tests, whose story branch is at a tip.
type aLandingAtATip struct {
	*fakeRetryLanding
	tip string
}

func (l aLandingAtATip) RefHead(ctx context.Context, dir, ref string) (string, error) {
	if ref == application.StoryBranch("mw-x.1") {
		return l.tip, nil
	}
	return l.fakeRetryLanding.RefHead(ctx, dir, ref)
}

// specsCounted is the rig's changed-specs command, which counts its runs.
type specsCounted struct{ runs int }

func (s *specsCounted) RunSpecs(context.Context, string, string, string) (application.SpecsChecked, error) {
	s.runs++
	return application.SpecsChecked{Checked: application.Checked{Command: "make specs", Passed: true}, Specs: []string{"a.feature"}}, nil
}

func aStoryOnTheLaptop() *apptest.FakeTracker {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	return tracker
}

// A whole check whose tests ran past its deadline finds the gate lock held by
// another gate when the changed specs ask for it: it ends at once in the one
// line, not after another nine minutes, and keeps that its tests passed.
func TestAWholeChecksChangedSpecsMeetingAHeldGateLockAfterTheTestsEndTheCallInOneLine(t *testing.T) {
	const deadline = 300 * time.Millisecond
	checks := &slowChecks{took: 400 * time.Millisecond, pass: true}
	w := newWholeCheckBesideTheGateLock(t, checks)
	// Another gate takes the lock the moment the tests give it back.
	var other application.GateTurn
	lock := &gateHandedOnAtFirstRelease{Gate: w.gate, then: func() {
		other, _ = w.gate.Take(context.Background(), "mw-gq6.894 closing out", nil)
	}}
	specs := &specsCounted{}
	landing := aLandingAtATip{&fakeRetryLanding{AheadCount: 1}, "abc1234"}
	check := func() (application.CheckReport, time.Duration, error) {
		started := time.Now()
		report, err := application.Check{
			Tracker: aStoryOnTheLaptop(), Landing: landing, Checks: checks, SpecChecks: specs,
			Slot: w.slots, GateLock: lock, Records: w.records, PartSlotWait: deadline,
			GateNoticeEvery: 40 * time.Millisecond, Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(w.told),
		}.Run(context.Background(), "mw-x.1")
		return report, time.Since(started), err
	}

	report, took, err := check()
	if err == nil {
		t.Fatalf("expected the check not to pass while the gate lock was held\n%s", report)
	}
	// No real-time margin is asserted (the host's load makes any of them flaky):
	// the specs' Take, the second one, was given a wait already spent, so the call
	// could not have waited for the other gate.
	if spent := lock.spent(); len(spent) != 2 || spent[0] || !spent[1] {
		t.Errorf("expected the tests' lock to be waited for and the specs' lock to be asked with the deadline spent, got spent-at-take %v (tests %s, deadline %s, call %s)", spent, checks.took, deadline, took)
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the changed specs did not start, got %+v", report.Refusals)
	}
	for _, want := range []string{"changed specs did not start", "gate lock is still held by", "mw-gq6.894", "since ", "nothing was run", "`mw check mw-x.1`"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("expected the line to say %q, got %q", want, refusal.Why)
		}
	}
	if strings.Contains(refusal.Why, "\n") {
		t.Errorf("expected one line, got %q", refusal.Why)
	}
	if specs.runs != 0 || checks.runs != 1 {
		t.Errorf("expected the specs not to run and the tests to run once, got %d and %d", specs.runs, checks.runs)
	}

	// The tests' pass is kept: the check run again after the other gate ends runs
	// the specs and not the tests.
	other.Holding.Release(context.Background())
	report, _, err = check()
	if err != nil {
		t.Fatalf("expected the resumed check to pass, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the resumed check not to run the tests again, they ran %d times", checks.runs)
	}
	if specs.runs != 1 {
		t.Errorf("expected the resumed check to run the changed specs, they ran %d times", specs.runs)
	}
	if !strings.Contains(report.String(), "not run again") {
		t.Errorf("expected the report to say the tests were not run again, got\n%s", report)
	}
}

// The tests' pass is kept only for the commit it was made at: a commit made
// since means the tests run again.
func TestAKeptPassOfAWholeChecksTestsIsNotUsedForAnotherCommit(t *testing.T) {
	checks := &slowChecks{pass: true}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.records.PutRunFile(context.Background(), "mw-x.1", application.CheckTestsKeptFileName(1), "tip 1111111\nmake test\n")
	specs := &specsCounted{}
	report, err := application.Check{
		Tracker: aStoryOnTheLaptop(), Landing: aLandingAtATip{&fakeRetryLanding{AheadCount: 1}, "2222222"}, Checks: checks, SpecChecks: specs,
		Slot: w.slots, GateLock: w.gate, Records: w.records, Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
	if err != nil {
		t.Fatalf("expected the check to pass, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run for a commit they were not kept for, they ran %d times", checks.runs)
	}
}

// The second wait for a calm host (the rerun after the tests failed while the
// host was busy) is under the deadline too: with it spent by the tests, the call
// ends in the one line and the tests are not run on a busy host.
func TestAWholeChecksSecondCalmWaitAfterTheTestsIsUnderTheDeadlineAndEndsInOneLine(t *testing.T) {
	const deadline = 300 * time.Millisecond
	checks := &slowChecks{took: 400 * time.Millisecond, pass: false}
	var calmWaited time.Duration
	report, err := application.Check{
		Tracker: aStoryOnTheLaptop(), Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		PartSlotWait: deadline, Records: &mapRecords{},
		Load:     &apptest.FakeHostLoad{Readings: []application.LoadReading{calm(), farOver()}},
		LoadPoll: 30 * time.Second,
		LoadWait: func(_ context.Context, d time.Duration) error { calmWaited += d; return nil },
		Host:     "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": t.TempDir()}, Out: writerOf(&lockedBuffer{}),
	}.Run(context.Background(), "mw-x.1")
	if err == nil {
		t.Fatalf("expected the check not to pass\n%s", report)
	}
	if calmWaited != 0 {
		t.Errorf("expected no wait for the host to calm, the deadline being spent, it waited %s", calmWaited)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests not to be run again on a busy host, they ran %d times", checks.runs)
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the rerun did not start, got %+v", report.Refusals)
	}
	for _, want := range []string{"run again", "did not start", "load 45.0 of 20 cores", "since ", "failed", "`mw check mw-x.1`"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("expected the line to say %q, got %q", want, refusal.Why)
		}
	}
	if strings.Contains(refusal.Why, "\n") {
		t.Errorf("expected one line, got %q", refusal.Why)
	}
}
