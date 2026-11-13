package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	// handBackAtOnce is Check.HandBackAtOnce.
	handBackAtOnce bool
	// keep is Check.KeepPlace: how long a wait that runs out keeps the check's
	// place in the queue (mw-gq6.939).
	keep time.Duration
	// clock is what Check.CallNow reads: the fit of the gate to the Builder's call
	// is judged by it, moved by hand, never by how long the host took (mw-gq6.983).
	clock *callClock
}

// callClock is the clock the fit of a gate or a part to a Builder's call is read
// by in these tests. It stands still until a test moves it, so that a test says
// "the wait took 1.5 s of the call" without sleeping 1.5 s and without a loaded
// host being able to eat the margin the test leaves (mw-gq6.983). Each read can
// also step it, to stand for a host that is slow to start.
type callClock struct {
	mu    sync.Mutex
	at    time.Time
	step  time.Duration
	reads int
}

func newCallClock() *callClock {
	return &callClock{at: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)}
}

// Now is Check.CallNow.
func (c *callClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.at
	c.at = c.at.Add(c.step)
	c.reads++
	return now
}

// PassOnceQueued moves the clock on by d once the call is queued (queued, the
// count of tickets in the queue it waits in, is at least one), which is to say once
// the call has read the clock for the start of its call and is waiting in line: a
// wait that the clock is moved by must not be over before the call is waiting, and
// a call read after the move would count the move as time before it began. The
// reads of the clock alone do not say so (the first is the start of the check's
// deadline, ahead of the start of its call), nor does a sleep; the ticket does
// (mw-gq6.1184).
func (c *callClock) PassOnceQueued(d time.Duration, queued func() int) {
	for deadline := time.Now().Add(10 * time.Second); queued() < 1 && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
	}
	c.Pass(d)
}

// Pass moves the clock on by d.
func (c *callClock) Pass(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// The clock is not moved by the queued-wait helper until a ticket is in place, so
// a call cannot be handed a move it counts as time before it began (mw-gq6.1184).
func TestTheCallClockIsMovedOnlyOnceATicketIsQueued(t *testing.T) {
	clock := newCallClock()
	var tickets atomic.Int32
	moved := make(chan struct{})
	go func() {
		clock.PassOnceQueued(time.Hour, func() int { return int(tickets.Load()) })
		close(moved)
	}()
	select {
	case <-moved:
		t.Fatal("expected the clock to stand still while nothing was queued")
	case <-time.After(50 * time.Millisecond):
	}
	if got := clock.Now(); !got.Equal(newCallClock().at) {
		t.Fatalf("expected the clock not to have moved, it reads %s", got)
	}
	tickets.Store(1)
	select {
	case <-moved:
	case <-time.After(10 * time.Second):
		t.Fatal("expected the clock to be moved once a ticket was queued")
	}
	if got := clock.Now(); got.Sub(newCallClock().at) != time.Hour {
		t.Errorf("expected the clock an hour on, it reads %s", got)
	}
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
		clock:   newCallClock(),
	}
}

func (w *aWholeCheckBesideTheHostsGateLock) check(wait time.Duration) (application.CheckReport, error) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	return application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: wait,
		GateNoticeEvery: 40 * time.Millisecond, HandBackAtOnce: w.handBackAtOnce, KeepPlace: w.keep,
		Host: "laptop", Remote: "origin",
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
	report, err := w.check(1500 * time.Millisecond)
	if err == nil {
		t.Fatalf("expected the check not to pass while a close-out's gate held the host's gate lock\n%s", report)
	}
	if time.Since(started) > 30*time.Second {
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

// releaseOnceTold gives the held gate lock back once the check has said in its
// output that it is waiting (the text want), and not after a stretch of real time:
// a host so loaded that the check starts after such a stretch finds the lock free,
// never waits and never says so, and the test fails though nothing is wrong
// (mw-gq6.1257). The bound is long and only a check that never waits meets it; the
// lock is then given back so that the check ends, and the test fails on the notice.
func (w *aWholeCheckBesideTheHostsGateLock) releaseOnceTold(holding application.Holding, want string) {
	go func() {
		for deadline := time.Now().Add(30 * time.Second); !strings.Contains(w.told.String(), want) && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		}
		holding.Release(context.Background())
	}()
}

func TestACheckWaitingForTheHostsGateLockRunsOnceItIsGivenBack(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.releaseOnceTold(closeOut.Holding, "gate lock is held by mw-gq6.894")
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

// slowGateLock is the host's gate lock after a real wait of at least held, which
// it records: when it was asked, how long it really waited, and the deadline of
// the context it was given. The test decides by those, not by how long the host
// took to do anything else, so a loaded host cannot fail it.
type slowGateLock struct {
	inner    *hostlock.Gate
	held     time.Duration
	asked    time.Time
	waited   time.Duration
	deadline time.Time
	bounded  bool
}

func (s *slowGateLock) Take(ctx context.Context, holder string, held func(by string)) (application.GateTurn, error) {
	s.asked = time.Now()
	s.deadline, s.bounded = ctx.Deadline()
	select {
	case <-time.After(s.held):
	case <-ctx.Done():
	}
	s.waited = time.Since(s.asked)
	return s.inner.Take(ctx, holder, held)
}

// The wait for the gate lock and the wait for a calm host that follows it are one
// deadline, the check's wait, so that together they stay under the 590 seconds a
// Builder's foreground call is given (mw-gq6.915). The deadline is an hour, far
// over anything a loaded host could spend, and every claim is a bound that the
// time the lock really waited or the check really took can only make true
// (mw-gq6.921).
func TestAWholeChecksWaitForTheGateLockAndForACalmHostAreOneDeadlineAndTheSecondGetsWhatTheFirstLeft(t *testing.T) {
	const deadline = time.Hour
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	lock := &slowGateLock{inner: w.gate, held: 250 * time.Millisecond}
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	// The calm host wait is counted, not slept.
	var calmWaited time.Duration
	started := time.Now()
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: w.slots, GateLock: lock, Records: w.records, PartSlotWait: deadline,
		GateNoticeEvery: 40 * time.Millisecond,
		Load:            &apptest.FakeHostLoad{Readings: []application.LoadReading{busy()}},
		LoadPoll:        30 * time.Second,
		LoadWait:        func(_ context.Context, d time.Duration) error { calmWaited += d; return nil },
		Host:            "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
	ran := time.Since(started)
	if err == nil {
		t.Fatalf("expected the check not to pass while the host stayed busy\n%s", report)
	}
	if lock.asked.IsZero() {
		t.Fatalf("expected the gate lock to be asked for\n%s", report)
	}
	// The lock wait ends at the check's one deadline: the context it is given has
	// a deadline, no more than the whole deadline from when it was asked, and no
	// less than what was left of it after the time before it was asked.
	if !lock.bounded {
		t.Fatalf("expected the lock wait to have the check's deadline, its context had none")
	}
	// The budget the check hands the wait is rounded to the millisecond, so it may
	// be up to half a millisecond under: a millisecond is allowed for it, as the
	// calm wait's is (mw-gq6.1184).
	if left := lock.deadline.Sub(lock.asked); left > deadline || left < deadline-lock.asked.Sub(started)-time.Millisecond {
		t.Errorf("expected the lock wait to end at the check's one deadline %s, it was given %s (asked %s into the check)",
			deadline, left, lock.asked.Sub(started))
	}
	// The calm wait gets what the lock wait left: the deadline less the time the
	// lock really waited (a millisecond over for the rounding of the budget), and
	// no less than the deadline less all the check took.
	if left := deadline - lock.waited; calmWaited > left+time.Millisecond {
		t.Errorf("expected the calm wait to get only what the lock wait left, at most %s, it waited %s (lock waited %s)",
			left, calmWaited, lock.waited)
	}
	if calmWaited < deadline-ran-time.Millisecond {
		t.Errorf("expected the calm wait to get what the lock wait left, at least %s, it waited %s", deadline-ran, calmWaited)
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the tests were not run, got %+v", report.Refusals)
	}
	for _, want := range []string{"did not start", "load 39.0 of 20 cores", "since ", "nothing was run", "`mw check mw-x.1`"} {
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
}

// A check queued behind a long gate waits on, as long as its limit allows, and
// says how long it waited and for whom (mw-gq6.938).
func TestAWholeCheckQueuedBehindALongGateRunsOnceItFreesAndSaysHowLongItWaitedAndForWhom(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.releaseOnceTold(closeOut.Holding, "gate lock is held by mw-5r3p30.1028")
	report, err := w.check(10 * time.Second)
	if err != nil {
		t.Fatalf("expected the check to wait for the gate lock and pass, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run once, they ran %d time(s)", checks.runs)
	}
	noted := strings.Join(report.Notes, "\n")
	for _, want := range []string{"waited", "mw-5r3p30.1028"} {
		if !strings.Contains(noted, want) {
			t.Errorf("expected the report to say it waited for the holder (%q), got notes %q", want, noted)
		}
	}
}

// When its limit is reached a check with HandBackAtOnce says so in one line
// naming the holder, and tells the Builder to hand back, not to run it again.
func TestAWholeCheckThatReachesItsWaitLimitSaysSoAndTellsTheBuilderToHandBackAtOnce(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.handBackAtOnce = true
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOut.Holding.Release(context.Background())

	for attempt := 1; attempt <= 2; attempt++ {
		report, err := w.check(100 * time.Millisecond)
		if err == nil {
			t.Fatalf("expected the check not to pass, got\n%s", report)
		}
		refusal := slotRefusalOf(report)
		if refusal == nil {
			t.Fatalf("expected a refusal that the tests were not run, got %+v", report.Refusals)
		}
		for _, want := range []string{"wait limit", "mw-5r3p30.1028", "nothing was run", "hand back", "do not run"} {
			if !strings.Contains(refusal.Why, want) {
				t.Errorf("attempt %d: expected the line to say %q, got %q", attempt, want, refusal.Why)
			}
		}
		if strings.Contains(refusal.Why, "\n") {
			t.Errorf("attempt %d: expected one line, got %q", attempt, refusal.Why)
		}
		if strings.Contains(refusal.Why, "run the same check again") {
			t.Errorf("attempt %d: expected no advice to run the check again, got %q", attempt, refusal.Why)
		}
	}
}

// queuedGate counts the tickets in the gate lock's queue directory.
func (w *aWholeCheckBesideTheHostsGateLock) queuedGate() int {
	return ticketsInPlace(filepath.Join(w.gate.Dir, hostlock.GateQueueDir))
}

// ticketsInPlace counts the tickets in place in a queue directory: not one still
// being made, which is under a dot name until it is locked and renamed.
func ticketsInPlace(queue string) int {
	entries, _ := os.ReadDir(queue)
	n := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			n++
		}
	}
	return n
}

// A ticket still being made is under a dot name until it is locked and renamed,
// and the queue lists no dot names, so a wait for tickets to be queued must not
// count it (mw-gq6.951).
func TestAWaitForQueuedTicketsDoesNotCountATicketStillBeingMade(t *testing.T) {
	w := newWholeCheckBesideTheGateLock(t, &scriptedChecks{passes: []bool{true}})
	gateQueue := filepath.Join(w.gate.Dir, hostlock.GateQueueDir)
	if err := os.MkdirAll(gateQueue, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gateQueue, ".being-made"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.queuedGate(); got != 0 {
		t.Errorf("expected a gate ticket still being made not to be counted, got %d", got)
	}
	if err := os.WriteFile(filepath.Join(gateQueue, "in-place"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.queuedGate(); got != 1 {
		t.Errorf("expected only the ticket in place to be counted, got %d", got)
	}

	p := newPartBesideTheMergeSlot(t)
	slotQueue := rig.SlotPath(p.rigDir) + rig.SlotQueueSuffix
	if err := os.MkdirAll(slotQueue, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slotQueue, ".being-made"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := p.queued(); got != 0 {
		t.Errorf("expected a slot ticket still being made not to be counted, got %d", got)
	}
}

// A Builder's foreground call is cut off at 590 seconds with no words, so a
// foreground check ends before that, in one line, with exit 0 and nothing run,
// and keeps its place in the queue for the next call (mw-gq6.939).
func TestAForegroundWholeCheckThatCannotGetTheGateLockInItsWaitEndsInOneLineKeepingItsPlaceAndSaysToRunItAgain(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOut.Holding.Release(context.Background())

	report, err := w.check(150 * time.Millisecond)
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if len(report.Refusals) != 0 {
		t.Errorf("expected no refusal, a kept place is not a failure, got %+v", report.Refusals)
	}
	for _, want := range []string{"gate lock is still held by mw-5r3p30.1028, since ", "your place 1 of 1 in the queue is kept for 10 minutes", "run the same command again to go on waiting"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if strings.Contains(report.Waiting, "\n") {
		t.Errorf("expected one line, got %q", report.Waiting)
	}
	if !strings.Contains(w.told.String(), report.Waiting) {
		t.Errorf("expected the line to be printed, got %q", w.told.String())
	}
	if strings.Contains(w.told.String(), "REFUSED") || strings.Contains(w.told.String(), "failed") {
		t.Errorf("expected no refusal or failure to be printed, got %q", w.told.String())
	}
	if checks.runs != 0 {
		t.Errorf("expected nothing to be run, the tests ran %d time(s)", checks.runs)
	}
	if got := w.queuedGate(); got != 1 {
		t.Errorf("expected the check's ticket to outlive the call, the queue holds %d", got)
	}
}

// ranAt is the rig's tests, noting when they were run.
type ranAt struct {
	at chan time.Time
}

func (r *ranAt) Run(context.Context, string, string) (application.Checked, error) {
	r.at <- time.Now()
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

// A check that returned and is run again within its keep time goes on with the
// place it had: ahead of a ticket taken in between, though the lock is free when
// that waiter arrives (mw-gq6.939).
func TestACheckRunAgainWithinItsKeepTimeHasItsPlaceAheadOfATicketTakenInBetween(t *testing.T) {
	checks := &ranAt{at: make(chan time.Time, 4)}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report, err := w.check(100 * time.Millisecond); err != nil || report.Waiting == "" {
		t.Fatalf("expected the first call to keep its place, got %v and %q", err, report.Waiting)
	}

	// Somebody joins the queue after the check went, and the holder lets go.
	gotAt := make(chan time.Time, 1)
	go func() {
		turn, err := w.gate.Take(context.Background(), "a close-out", nil)
		if err == nil {
			gotAt <- time.Now()
			time.Sleep(50 * time.Millisecond)
			turn.Holding.Release(context.Background())
		}
	}()
	for deadline := time.Now().Add(5 * time.Second); w.queuedGate() < 2 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if got := w.queuedGate(); got != 2 {
		t.Fatalf("expected the kept ticket and the new one in the queue, got %d", got)
	}
	closeOut.Holding.Release(context.Background())
	// The lock is free, and the ticket in between does not take it: the check's
	// is the head though nobody is at it.
	select {
	case <-gotAt:
		t.Fatal("expected the ticket taken in between to wait behind the kept place")
	case <-time.After(300 * time.Millisecond):
	}

	report, err := w.check(10 * time.Second)
	if err != nil {
		t.Fatalf("expected the second call to take its place and run, got %v\n%s", err, report)
	}
	ran := <-checks.at
	select {
	case got := <-gotAt:
		if got.Before(ran) {
			t.Errorf("expected the check to run before the ticket taken in between had the lock")
		}
	case <-time.After(10 * time.Second):
		t.Error("expected the ticket taken in between to have the lock once the check was done")
	}
	if got := w.queuedGate(); got != 0 {
		t.Errorf("expected no ticket left once both are done, got %d", got)
	}
}

// A check that keeps no place of its own (a background one, which waits as long
// as it is allowed) still takes up the place an earlier foreground check of the
// same story kept, and does not queue behind it, so that it does not wait for
// itself (mw-gq6.1196).
func TestACheckThatKeepsNoPlaceTakesUpThePlaceAForegroundCheckOfTheSameStoryKept(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report, err := w.check(100 * time.Millisecond); err != nil || report.Waiting == "" {
		t.Fatalf("expected the first call to keep its place, got %v and %q", err, report.Waiting)
	}
	closeOut.Holding.Release(context.Background())
	if got := w.queuedGate(); got != 1 {
		t.Fatalf("expected the kept place to be in the queue, got %d", got)
	}

	// The same story's check, asked to keep no place, with the lock free: it takes
	// the kept place, which is at the head, and runs.
	w.keep = 0
	report, err := w.check(2 * time.Second)
	if err != nil {
		t.Fatalf("expected the check to take the story's own kept place and run, got %v\n%s", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run once, they ran %d time(s)", checks.runs)
	}
	if got := w.queuedGate(); got != 0 {
		t.Errorf("expected no ticket left, got %d", got)
	}
}

// A place that is not run again is given up when its keep time has passed.
func TestAKeptPlaceIsGivenUpWhenItsKeepTimeHasPassed(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 150 * time.Millisecond
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.check(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	closeOut.Holding.Release(context.Background())
	time.Sleep(300 * time.Millisecond)
	// Nobody comes back for it: a close-out's gate takes the lock at once.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	turn, err := w.gate.Take(ctx, "a close-out", nil)
	if err != nil {
		t.Fatalf("expected a kept place past its time not to hold the queue, got %v", err)
	}
	turn.Holding.Release(context.Background())
}

// The merge slot a landing holds ends a foreground check the same way: one line,
// the place kept, exit 0 (mw-gq6.939).
func TestAForegroundWholeCheckThatCannotGetTheMergeSlotKeepsItsPlaceAndSaysToRunItAgain(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	landing, err := w.slots.Take(context.Background(), w.rigDir, "mw land on laptop, landing mw-y")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Release(context.Background())

	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: 150 * time.Millisecond,
		KeepPlace: 10 * time.Minute, Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
	if err != nil || len(report.Refusals) != 0 {
		t.Fatalf("expected the check to end without a refusal or an error, got %v and %+v", err, report.Refusals)
	}
	for _, want := range []string{"the merge slot of millwright is still held by mw land on laptop, landing mw-y, since ", "your place 1 of 1 in the queue is kept for 10 minutes", "run the same command again to go on waiting"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if checks.runs != 0 {
		t.Errorf("expected nothing to be run, the tests ran %d time(s)", checks.runs)
	}
}

// A host that stays busy ends the check the same way, with no place to keep: no
// queue stands behind a busy host (mw-gq6.939).
func TestAForegroundWholeCheckOnAHostThatStaysBusyEndsInOneLineToRunItAgain(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: 3 * time.Minute,
		KeepPlace: 10 * time.Minute,
		Load:      &apptest.FakeHostLoad{Readings: []application.LoadReading{busy()}},
		LoadPoll:  30 * time.Second,
		LoadWait:  func(context.Context, time.Duration) error { return nil },
		Host:      "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
	if err != nil || len(report.Refusals) != 0 {
		t.Fatalf("expected the check to end without a refusal or an error, got %v and %+v", err, report.Refusals)
	}
	for _, want := range []string{"laptop is still at load 39.0 of 20 cores, since ", "run the same command again to go on waiting"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if checks.runs != 0 {
		t.Errorf("expected nothing to be run, the tests ran %d time(s)", checks.runs)
	}
}

// check is w.check with the call's budget and the rig's expected gate given
// (mw-gq6.940).
func (w *aWholeCheckBesideTheHostsGateLock) checkInACall(wait, budget, expected time.Duration) (application.CheckReport, error) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	return application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: wait,
		GateNoticeEvery: 40 * time.Millisecond, KeepPlace: w.keep,
		CallBudget: budget, CallNow: w.clock.Now,
		GateExpected: func(_ context.Context, rig string) time.Duration { return expected },
		Host:         "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
}

// A foreground check that waited for the gate lock and got it too late for the
// rig's gate to end inside the Builder's call does not start the gate, which the
// call would cut off silently with the lock held: it keeps its place, says what
// the gate needs and what is left, and ends exit 0 (mw-gq6.940). Run again at
// once, the same command takes the place up and starts the gate.
//
// The call's clock is moved by hand (callClock): the lock is held for 1.5 s of
// it, the gate expected is 9 s of a 10 s call, so the check is late and the
// second call, which starts with all its budget, fits, whatever the host took
// time over (mw-gq6.983).
func TestAForegroundWholeCheckThatGetsTheLockTooLateForTheGateKeepsItsPlaceAndSaysToRunItAgain(t *testing.T) {
	checks := &ranAt{at: make(chan time.Time, 4)}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1028", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		w.clock.PassOnceQueued(1500*time.Millisecond, w.queuedGate)
		closeOut.Holding.Release(context.Background())
	}()

	report, err := w.checkInACall(time.Minute, 10*time.Second, 9*time.Second)
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if len(report.Refusals) != 0 {
		t.Errorf("expected no refusal, got %+v", report.Refusals)
	}
	for _, want := range []string{"the gate needs about 9s and only ", " left in this call", "your place 1 of 1 in the queue is kept for 10 minutes", "run the same command again", "`mw check mw-x.1 --background`"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if strings.Contains(report.Waiting, "\n") {
		t.Errorf("expected one line, got %q", report.Waiting)
	}
	if !strings.Contains(w.told.String(), report.Waiting) {
		t.Errorf("expected the line to be printed, got %q", w.told.String())
	}
	select {
	case <-checks.at:
		t.Fatal("expected the gate not to be started")
	default:
	}
	if got := w.queuedGate(); got != 1 {
		t.Errorf("expected the check's ticket to outlive the call, the queue holds %d", got)
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the gate lock to be given back, %q holds it", holder)
	}

	// A close-out's gate joins behind the kept place; the check run again at once
	// takes its place up and runs ahead of it, without queuing again.
	gotAt := make(chan time.Time, 1)
	go func() {
		turn, err := w.gate.Take(context.Background(), "a close-out", nil)
		if err == nil {
			gotAt <- time.Now()
			turn.Holding.Release(context.Background())
		}
	}()
	for deadline := time.Now().Add(5 * time.Second); w.queuedGate() < 2 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if got := w.queuedGate(); got != 2 {
		t.Fatalf("expected the kept ticket and the close-out's in the queue, got %d", got)
	}
	select {
	case <-gotAt:
		t.Fatal("expected the close-out to wait behind the kept place")
	case <-time.After(200 * time.Millisecond):
	}

	report, err = w.checkInACall(time.Minute, 10*time.Second, 9*time.Second)
	if err != nil || report.Waiting != "" {
		t.Fatalf("expected the second call to start the gate, got %v and %q\n%s", err, report.Waiting, report)
	}
	ran := <-checks.at
	if !strings.Contains(w.told.String(), "expected about 9s, ") || !strings.Contains(w.told.String(), " left in this call") {
		t.Errorf("expected the figures to be said when the gate starts, got %q", w.told.String())
	}
	select {
	case got := <-gotAt:
		if got.Before(ran) {
			t.Errorf("expected the check to run before the close-out had the lock")
		}
	case <-time.After(10 * time.Second):
		t.Error("expected the close-out to have the lock once the check was done")
	}
}

// checkInACallAfterACalmWait is checkInACall on a host that is busy at the first
// reading and calm at the next, the wait between them taking calmFor of real time
// (mw-gq6.945).
func (w *aWholeCheckBesideTheHostsGateLock) checkInACallAfterACalmWait(wait, budget, expected, calmFor time.Duration) (application.CheckReport, error) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	return application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: wait,
		GateNoticeEvery: 40 * time.Millisecond, KeepPlace: w.keep,
		CallBudget: budget, CallNow: w.clock.Now,
		GateExpected: func(_ context.Context, rig string) time.Duration { return expected },
		Load:         &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}},
		LoadPoll:     time.Second,
		LoadWait:     func(_ context.Context, d time.Duration) error { w.clock.Pass(calmFor); return nil },
		Host:         "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
}

// The fit is decided when the gate truly starts: a check that won the gate lock
// with time to spare, and then waited for a calm host so long that the gate no
// longer ends inside the call, does not start the gate either. It gives the lock
// back, keeps its place and says to run the same command again (mw-gq6.945).
// Run again on a calm host, it takes its place up and starts the gate.
//
// The call's clock is moved by hand (callClock): the gate is expected to take
// 9 s of a 10 s call and the calm wait takes 1.5 s of it, so the lock is won in
// time and the gate is late after the wait, and the second call fits, whatever
// the host took time over. Before mw-gq6.983 the times were real, and a host
// that took a second to start the second call made it late.
func TestAForegroundWholeCheckWhoseCalmWaitEatsTheFitKeepsItsPlaceAndSaysToRunItAgain(t *testing.T) {
	checks := &ranAt{at: make(chan time.Time, 4)}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute

	report, err := w.checkInACallAfterACalmWait(time.Minute, 10*time.Second, 9*time.Second, 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if len(report.Refusals) != 0 {
		t.Errorf("expected no refusal, got %+v", report.Refusals)
	}
	for _, want := range []string{"the gate needs about 9s and only ", " left in this call", "your place 1 of 1 in the queue is kept for 10 minutes", "run the same command again", "`mw check mw-x.1 --background`"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if strings.Contains(report.Waiting, "\n") {
		t.Errorf("expected one line, got %q", report.Waiting)
	}
	if !strings.Contains(w.told.String(), report.Waiting) {
		t.Errorf("expected the line to be printed, got %q", w.told.String())
	}
	select {
	case <-checks.at:
		t.Fatal("expected the gate not to be started")
	default:
	}
	if got := w.queuedGate(); got != 1 {
		t.Errorf("expected the check's ticket to outlive the call, the queue holds %d", got)
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the gate lock to be given back, %q holds it", holder)
	}

	// A close-out's gate joins behind the kept place; the check run again on a calm
	// host takes its place up and runs ahead of it.
	gotAt := make(chan time.Time, 1)
	go func() {
		turn, err := w.gate.Take(context.Background(), "a close-out", nil)
		if err == nil {
			gotAt <- time.Now()
			turn.Holding.Release(context.Background())
		}
	}()
	for deadline := time.Now().Add(5 * time.Second); w.queuedGate() < 2 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-gotAt:
		t.Fatal("expected the close-out to wait behind the kept place")
	case <-time.After(200 * time.Millisecond):
	}
	report, err = w.checkInACall(time.Minute, 10*time.Second, 9*time.Second)
	if err != nil || report.Waiting != "" {
		t.Fatalf("expected the second call to start the gate, got %v and %q\n%s", err, report.Waiting, report)
	}
	ran := <-checks.at
	select {
	case got := <-gotAt:
		if got.Before(ran) {
			t.Errorf("expected the check to run before the close-out had the lock")
		}
	case <-time.After(10 * time.Second):
		t.Error("expected the close-out to have the lock once the check was done")
	}
}

// The fit is judged by the call's clock (Check.CallNow) and by nothing else: a
// call whose clock runs 2 s on at each reading is late for a 9 s gate in a 10 s
// call, with the gate lock free and no wait at all, and a call whose clock does
// not move is never late however long the host takes (mw-gq6.983). This is how
// the tests above stand for a wait without sleeping through it: a loaded host
// that took a second over the second call of one of them made it late, and the
// test failed (seen once, on mw-gq6.692's landing).
func TestAForegroundWholeCheckIsLateByTheCallsClockAndNotByTheTimeTheHostTook(t *testing.T) {
	checks := &ranAt{at: make(chan time.Time, 4)}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute
	w.clock.step = 2 * time.Second

	report, err := w.checkInACall(time.Minute, 10*time.Second, 9*time.Second)
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if !strings.Contains(report.Waiting, "the gate needs about 9s and only ") || !strings.Contains(report.Waiting, "your place 1 of 1 in the queue is kept for 10 minutes") {
		t.Errorf("expected the line to say the gate is late and the place is kept, got %q", report.Waiting)
	}
	select {
	case <-checks.at:
		t.Fatal("expected the gate not to be started")
	default:
	}

	// The same call on a clock that stands still, with the host taking a real
	// second over the gate lock first, is on time.
	w2 := newWholeCheckBesideTheGateLock(t, checks)
	holder, err := w2.gate.Take(context.Background(), "a close-out", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(1100 * time.Millisecond)
		holder.Holding.Release(context.Background())
	}()
	report, err = w2.checkInACall(time.Minute, 10*time.Second, 9*time.Second)
	if err != nil || report.Waiting != "" {
		t.Fatalf("expected a call whose clock stood still to start the gate, got %v and %q\n%s", err, report.Waiting, report)
	}
	<-checks.at
}

// A calm wait that leaves the gate room inside the call changes nothing: the gate
// is started, with the figures said (mw-gq6.945).
func TestAForegroundWholeCheckWhoseCalmWaitLeavesTheFitStartsTheGate(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.keep = 10 * time.Minute
	report, err := w.checkInACallAfterACalmWait(time.Minute, 10*time.Second, 3*time.Second, 1500*time.Millisecond)
	if err != nil || report.Waiting != "" {
		t.Fatalf("expected the gate to be started, got %v and %q\n%s", err, report.Waiting, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected the tests to run once, ran %d", checks.runs)
	}
	if !strings.Contains(w.told.String(), "expected about 3s, ") {
		t.Errorf("expected the figures to be said when the gate starts, got %q", w.told.String())
	}
}

// A gate that cannot end inside even a whole call is started whatever time is
// left by a check that cannot start a process of its own (no GateStarter):
// refusing it would refuse it for ever (a rig with no figure, or a figure past
// the call, is run as before). One that can runs it there (mw-gq6.1273,
// TestAForegroundCheckOnAGateLongerThanTheCall).
func TestAForegroundWholeCheckWhoseGateIsLongerThanAWholeCallStartsItAsBefore(t *testing.T) {
	for name, expected := range map[string]time.Duration{"no figure": 0, "figure past the call": time.Hour} {
		t.Run(name, func(t *testing.T) {
			checks := &scriptedChecks{passes: []bool{true}}
			w := newWholeCheckBesideTheGateLock(t, checks)
			w.keep = 10 * time.Minute
			report, err := w.checkInACall(time.Minute, 10*time.Second, expected)
			if err != nil || report.Waiting != "" {
				t.Fatalf("expected the gate to be started, got %v and %q", err, report.Waiting)
			}
			if checks.runs != 1 {
				t.Errorf("expected the tests to run once, ran %d", checks.runs)
			}
		})
	}
}

// quickParts is a rig's gate in two parts that each pass at once, noting which
// ran. Neither is a slot-free runner, so both take the rig's merge slot.
type quickParts struct {
	mu  sync.Mutex
	ran []string
}

func (q *quickParts) Parts(string) []application.GatePart {
	return []application.GatePart{{Name: "lint", Command: "lint"}, {Name: "unit", Command: "unit"}}
}

func (q *quickParts) RunLine(_ context.Context, _, _, command string) (application.Checked, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ran = append(q.ran, command)
	return application.Checked{Command: command, Passed: true}, nil
}

func (q *quickParts) ranCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.ran)
}

// aPartBesideTheMergeSlot is mw check --part N in a foreground call of budget,
// on a rig whose merge slot a landing may hold (mw-gq6.944).
type aPartBesideTheMergeSlot struct {
	slots   *rig.Slots
	rigDir  string
	parts   *quickParts
	records *mapRecords
	told    *lockedBuffer
	// clock is what Check.CallNow reads (mw-gq6.983).
	clock *callClock
}

func newPartBesideTheMergeSlot(t *testing.T) *aPartBesideTheMergeSlot {
	t.Helper()
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &aPartBesideTheMergeSlot{
		slots:   rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond)),
		rigDir:  rigDir,
		parts:   &quickParts{},
		records: &mapRecords{},
		told:    &lockedBuffer{},
		clock:   newCallClock(),
	}
}

func (p *aPartBesideTheMergeSlot) checkPart(n int, wait, budget time.Duration, expected func(part int) time.Duration) (application.CheckReport, error) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	return application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		Slot: p.slots, Parts: p.parts, Records: p.records, Part: n, PartSlotWait: wait,
		KeepPlace:  10 * time.Minute,
		CallBudget: budget, CallNow: p.clock.Now,
		PartExpected: func(_ context.Context, _ string, part int) time.Duration {
			if expected == nil {
				return 0
			}
			return expected(part)
		},
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": p.rigDir},
		Out:  writerOf(p.told),
	}.Run(context.Background(), "mw-x.1")
}

func (p *aPartBesideTheMergeSlot) queued() int {
	return ticketsInPlace(rig.SlotPath(p.rigDir) + rig.SlotQueueSuffix)
}

// A part that waited for the merge slot and won it too late to end inside the
// Builder's call does not start: the call would cut it off silently with the slot
// held. It gives the slot back, keeps its place and says what is left (mw-gq6.944).
// Run again at once, it takes the place up and runs.
//
// The call's clock is moved by hand (callClock): the landing holds the slot for
// 3 s of it, the part is expected to take 10 s of a 12 s call, so it is late, and
// the second call, which starts with all its budget, fits, whatever the host took
// time over (mw-gq6.983).
func TestAForegroundPartThatGetsTheMergeSlotTooLateForItKeepsItsPlaceAndSaysToRunItAgain(t *testing.T) {
	p := newPartBesideTheMergeSlot(t)
	landing, err := p.slots.Take(context.Background(), p.rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		p.clock.PassOnceQueued(3*time.Second, p.queued)
		landing.Release(context.Background())
	}()

	expected := func(part int) time.Duration {
		if part == 2 {
			return 10 * time.Second
		}
		return 0
	}
	_, err = p.checkPart(2, time.Minute, 12*time.Second, expected)
	if err == nil {
		t.Fatal("expected a part that did not start to say so")
	}
	for _, want := range []string{"part 2 of 2 (unit) did not start", "needs about 10s and only ", " left in this call", "your place 1 of 1 in the queue is kept for 10 minutes", "run the same command again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the line to say %q, got %q", want, err.Error())
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("expected one line, got %q", err.Error())
	}
	if p.parts.ranCount() != 0 {
		t.Errorf("expected the part not to be started, it ran %d time(s)", p.parts.ranCount())
	}
	if got := p.queued(); got != 1 {
		t.Errorf("expected the part's ticket to outlive the call, the queue holds %d", got)
	}
	if _, err := p.checkPart(2, time.Minute, 12*time.Second, expected); err != nil {
		t.Fatalf("expected the second call to run the part, got %v", err)
	}
	if p.parts.ranCount() != 1 {
		t.Errorf("expected the part to run once, it ran %d time(s)", p.parts.ranCount())
	}
	if got := p.queued(); got != 0 {
		t.Errorf("expected the place to be used up by the run, the queue holds %d", got)
	}
}

// A part that fits what is left of the call, or has no figure, or whose time is
// longer than a whole call, is started as it always was.
func TestAForegroundPartThatFitsOrHasNoFigureIsStarted(t *testing.T) {
	for name, expected := range map[string]time.Duration{
		"fits":                 2 * time.Second,
		"no figure":            0,
		"figure past the call": time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			p := newPartBesideTheMergeSlot(t)
			_, err := p.checkPart(1, time.Minute, 10*time.Second, func(int) time.Duration { return expected })
			if err != nil {
				t.Fatalf("expected the part to be started, got %v", err)
			}
			if p.parts.ranCount() != 1 {
				t.Errorf("expected the part to run once, it ran %d time(s)", p.parts.ranCount())
			}
			if expected > 0 && expected < 10*time.Second && !strings.Contains(p.told.String(), "expected about 2s, ") {
				t.Errorf("expected the figures to be said when the part starts, got %q", p.told.String())
			}
		})
	}
}

// A part's time is kept, by part number and host, for the next fit test to read
// (mw-gq6.944): not for a part that failed, whose time says nothing of a good run.
func TestAPartThatPassedKeepsItsTimeByPartNumberAndHost(t *testing.T) {
	p := newPartBesideTheMergeSlot(t)
	// Every read of the call's clock moves it a second, so the part's seconds are
	// the clock's and not the host's (mw-gq6.1217).
	p.clock.step = time.Second
	if _, err := p.checkPart(2, time.Minute, 0, nil); err != nil {
		t.Fatal(err)
	}
	text, err := p.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckPartTimeFileName(1, 2))
	if err != nil {
		t.Fatalf("expected the part's time to be kept: %v", err)
	}
	got, ok := application.ReadPartTime("mw-x.1", text)
	if !ok {
		t.Fatalf("expected the record to be read back, got %q", text)
	}
	if got.Part != 2 || got.Host != "laptop" || got.Rig != "millwright" || got.Story != "mw-x.1" || got.At.IsZero() {
		t.Errorf("expected part 2 of millwright on laptop with a time, got %+v", got)
	}
	if got.Seconds < 1 || got.Seconds != float64(int(got.Seconds)) {
		t.Errorf("expected the seconds the part took to be read off the call's clock, whole seconds from its steps, got %v", got.Seconds)
	}
}

func TestAPartsExpectedTimeIsTheMedianOfItsLastRunsOnTheHostAlone(t *testing.T) {
	at := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	run := func(host string, part int, secs float64, mins int) application.PartTime {
		return application.PartTime{Story: "mw-x", Rig: "millwright", Host: host, Part: part, At: at.Add(time.Duration(mins) * time.Minute), Seconds: secs}
	}
	history := []application.PartTime{
		run("desktop", 2, 100, 1), run("desktop", 2, 300, 2), run("desktop", 2, 200, 3),
		run("desktop", 1, 10, 4), run("laptop", 2, 999, 5),
	}
	s := application.BenchmarkSettings{}
	if got := application.GatePartExpectedFor(history, "millwright", "desktop", 2, s); got != 200*time.Second {
		t.Errorf("expected the median of part 2's three runs on the desktop, 200s, got %s", got)
	}
	if got := application.GatePartExpectedFor(history, "millwright", "desktop", 1, s); got != 10*time.Second {
		t.Errorf("expected part 1's own time, got %s", got)
	}
	if got := application.GatePartExpectedFor(history, "millwright", "desktop", 3, s); got != 0 {
		t.Errorf("expected no figure for a part never run, got %s", got)
	}
	if got := application.GatePartExpectedFor(history, "other", "desktop", 2, s); got != 0 {
		t.Errorf("expected no figure for another rig, got %s", got)
	}
	// Only the last UsualGates runs count.
	if got := application.GatePartExpectedFor(history, "millwright", "desktop", 2, application.BenchmarkSettings{UsualGates: 2}); got != 250*time.Second {
		t.Errorf("expected the median of the last two runs, 250s, got %s", got)
	}
}

// The wait for the host's gate lock is bounded by the check's clock (Check.CallNow)
// and not by the real time: with an hour of wait and the lock held, a clock moved
// past the hour ends the wait at once, with no sleep (mw-gq6.1142).
func TestAWholeChecksWaitForTheGateLockEndsWhenTheChecksClockPassesItsBound(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	holder, err := w.gate.Take(context.Background(), "mw-gq6.1142-holder", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Holding.Release(context.Background())
	go func() {
		for deadline := time.Now().Add(10 * time.Second); w.queuedGate() < 1 && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		}
		w.clock.Pass(2 * time.Hour)
	}()

	started := time.Now()
	report, err := w.checkInACall(time.Hour, time.Hour, 0)
	if err == nil {
		t.Fatalf("expected the check not to pass while the gate lock was held\n%s", report)
	}
	if took := time.Since(started); took > 20*time.Second {
		t.Errorf("expected the wait to end when the check's clock passed its bound, it took %s of real time", took)
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the tests were not run, got %+v", report.Refusals)
	}
	for _, want := range []string{"did not start", "gate lock", "mw-gq6.1142-holder", "nothing was run"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("expected the line to say %q, got %q", want, refusal.Why)
		}
	}
	if checks.runs != 0 {
		t.Errorf("expected the tests not to have run, they ran %d time(s)", checks.runs)
	}
}

// The same for the rig's merge slot, the other wait takeForTests makes.
func TestAWholeChecksWaitForTheMergeSlotEndsWhenTheChecksClockPassesItsBound(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	told := &lockedBuffer{}
	w.slots = rig.NewSlots(rig.WithSlotPoll(5*time.Millisecond), rig.WithSlotNoticeEvery(10*time.Millisecond),
		rig.WithSlotNotice(func(said string) { told.WriteString(said) }))
	landing, err := w.slots.Take(context.Background(), w.rigDir, "mw land, landing a batch")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Release(context.Background())
	go func() {
		for deadline := time.Now().Add(10 * time.Second); told.String() == "" && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		}
		w.clock.Pass(2 * time.Hour)
	}()

	started := time.Now()
	report, err := w.checkInACall(time.Hour, time.Hour, 0)
	if err == nil {
		t.Fatalf("expected the check not to pass while the merge slot was held\n%s", report)
	}
	if took := time.Since(started); took > 20*time.Second {
		t.Errorf("expected the wait to end when the check's clock passed its bound, it took %s of real time", took)
	}
	refusal := slotRefusalOf(report)
	if refusal == nil {
		t.Fatalf("expected a refusal that the tests were not run, got %+v", report.Refusals)
	}
	if !strings.Contains(refusal.Why, "merge slot") || !strings.Contains(refusal.Why, "nothing was run") {
		t.Errorf("expected the line to name the merge slot and that nothing was run, got %q", refusal.Why)
	}
	if checks.runs != 0 {
		t.Errorf("expected the tests not to have run, they ran %d time(s)", checks.runs)
	}
}

// slowStartTracker is the work tracker of a host at load 43: reading the story is
// the first thing a check does and each tracker call waits for the host to be
// quiet (mw-gq6.1251), so the call's clock has moved by the time the check knows
// which story it is checking.
type slowStartTracker struct {
	*apptest.FakeTracker
	clock *callClock
	took  time.Duration
}

func (s slowStartTracker) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	s.clock.Pass(s.took)
	return s.FakeTracker.ShowStory(ctx, id)
}

// checkAfterASlowStart is a foreground whole check whose first read of the story
// takes slow of the call's clock, with the call's budget and the wait limit both
// the default 540 seconds, as mw check gives a Builder's call.
func (w *aWholeCheckBesideTheHostsGateLock) checkAfterASlowStart(slow, expected time.Duration) (application.CheckReport, error) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	var gateExpected func(context.Context, string) time.Duration
	if expected > 0 {
		gateExpected = func(context.Context, string) time.Duration { return expected }
	}
	return application.Check{
		Tracker: slowStartTracker{FakeTracker: tracker, clock: w.clock, took: slow}, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: application.CheckPartSlotWait,
		GateNoticeEvery: 40 * time.Millisecond, KeepPlace: application.CheckKeepPlace,
		CallBudget: application.CheckCallBudget, CallNow: w.clock.Now, GateExpected: gateExpected,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir},
		Out:  writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
}

// A foreground check on a host so loaded that reading the story took two minutes
// of the call spends its wait out of what the call has left, not out of a nine
// minutes that began when the story was read: the wait is one deadline from the
// start of the call, so it ends in the place-kept line inside the tool's 600
// seconds and not in the tool's silence (mw-gq6.1251, which mw-gq6.939's fix did
// not hold for).
func TestAForegroundCheckEndsItsWaitInsideTheCallsBudgetOnASlowStart(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	released := make(chan struct{})
	go func() {
		defer close(released)
		// The clock is 120 s on when the check has read the story: 430 s more is
		// 550 s into a call whose budget is 540 s.
		w.clock.PassOnceQueued(430*time.Second, w.queuedGate)
		// A wait that is not bounded by the call's start goes on; the lock is
		// given back only so that such a check ends and the test can say so.
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		closeOut.Holding.Release(context.Background())
	}()

	report, err := w.checkAfterASlowStart(120*time.Second, 0)
	close(done)
	<-released
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if checks.runs != 0 {
		t.Fatalf("expected the wait to end at the call's budget with nothing run, the tests ran %d time(s)\n%s", checks.runs, report)
	}
	for _, want := range []string{"gate lock is still held by", "mw-gq6.894", "your place 1 of 1 in the queue is kept for 10 minutes", "run the same command again to go on waiting"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
}

// The gate's fit to the call is judged from the start of the call too: a gate
// expected to take 450 s does not fit what is left of a 540 s call after a start
// that took 120 s, and is not started (mw-gq6.1251).
func TestAForegroundCheckDoesNotStartAGateThatDoesNotFitWhatASlowStartLeftOfTheCall(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	report, err := w.checkAfterASlowStart(120*time.Second, 450*time.Second)
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if checks.runs != 0 {
		t.Fatalf("expected the gate not to start with 7m0s of the call left for a gate of 7m30s, the tests ran %d time(s)\n%s", checks.runs, report)
	}
	for _, want := range []string{"the gate needs about 7m30s and only ", " left in this call", "run the same command again"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
}

// loggedRecords is a place for the check's log: the records, with a path for the
// log file.
type loggedRecords struct{ *mapRecords }

func (l loggedRecords) RunFile(story, name string) string { return story + "/" + name }

// A check the tool kills while it waits leaves its log: the line that says it
// began and each line the wait said are in runs/<story>/check-<attempt>.log as
// they are printed, not only when the check ends (mw-gq6.1251).
func TestAForegroundCheckWritesItsLogWhileItWaitsAndNotOnlyWhenItEnds(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	closeOut, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	logs := loggedRecords{&mapRecords{}}
	var midWait string
	go func() {
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if text, err := logs.ReadRunFile(context.Background(), "mw-x.1", application.CheckLogFileName(1)); err == nil && strings.Contains(text, "still waiting for laptop's gate lock") {
				midWait = text
				break
			}
		}
		w.clock.Pass(time.Hour)
	}()

	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, Logs: logs, PartSlotWait: application.CheckPartSlotWait,
		GateNoticeEvery: 40 * time.Millisecond, KeepPlace: application.CheckKeepPlace,
		CallBudget: application.CheckCallBudget, CallNow: w.clock.Now,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
	closeOut.Holding.Release(context.Background())
	if err != nil {
		t.Fatalf("expected a check that kept its place to end without an error, got %v\n%s", err, report)
	}
	if midWait == "" {
		t.Fatal("expected the log to say, while the check still waited, that it was waiting")
	}
	for _, want := range []string{"check of mw-x.1 (attempt 1) began on laptop at ", "held by mw-gq6.894"} {
		if !strings.Contains(midWait, want) {
			t.Errorf("expected the log, read while the check waited, to say %q, got %q", want, midWait)
		}
	}
	final, _ := logs.ReadRunFile(context.Background(), "mw-x.1", application.CheckLogFileName(1))
	if !strings.HasPrefix(final, "check of mw-x.1 (attempt 1) began on laptop") || !strings.Contains(final, report.Waiting) {
		t.Errorf("expected the log at the end to hold the whole check, got %q", final)
	}
}

// The wait for a calm host counts the steps it slept, and the reads and the late
// wakes of a host at load 43 are not among them: a call whose deadline has passed
// by its clock ends the wait after the step that passed it, not after the steps
// the count still owes (mw-gq6.1251).
func TestAForegroundCheckEndsItsCalmWaitWhenTheCallsClockSaysTheDeadlinePassed(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	var slept int
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: 3 * time.Minute,
		KeepPlace: application.CheckKeepPlace, CallBudget: 3 * time.Minute, CallNow: w.clock.Now,
		Load:     &apptest.FakeHostLoad{Readings: []application.LoadReading{busy()}},
		LoadPoll: 30 * time.Second,
		// Each sleep takes the whole call, though the count says 30 seconds.
		LoadWait: func(context.Context, time.Duration) error { slept++; w.clock.Pass(5 * time.Minute); return nil },
		Host:     "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(w.told),
	}.Run(context.Background(), "mw-x.1")
	if err != nil || len(report.Refusals) != 0 {
		t.Fatalf("expected the check to end without a refusal or an error, got %v and %+v", err, report.Refusals)
	}
	if slept != 1 {
		t.Errorf("expected one sleep, which passed the call's deadline, got %d", slept)
	}
	if !strings.Contains(report.Waiting, "run the same command again to go on waiting") || checks.runs != 0 {
		t.Errorf("expected the check to end in the run-again line with nothing run, got %q and %d run(s)", report.Waiting, checks.runs)
	}
}

// gateTakingTheCall is the rig's gate on a host where it usually takes longer
// than a whole call (cloud8's 12m47s against the call's 540 s, mw-gq6.1273): a run
// moves the call's clock on by took, as the tool's own clock would move. It is the
// gate as the call would run it; the process the call starts runs heldGate.
type gateTakingTheCall struct {
	clock *callClock
	took  time.Duration
	mu    sync.Mutex
	runs  int
}

func (g *gateTakingTheCall) Run(context.Context, string, string) (application.Checked, error) {
	g.mu.Lock()
	g.runs++
	g.mu.Unlock()
	g.clock.Pass(g.took)
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

func (g *gateTakingTheCall) ranCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs
}

// heldGate is the rig's gate as the process of a whole check runs it, which the
// test holds: a run says it has started and stays until the test lets it go,
// passing or failing as the test says.
type heldGate struct {
	mu      sync.Mutex
	runs    int
	passed  bool
	started chan struct{}
	release chan struct{}
}

func newHeldGate(passed bool) *heldGate {
	return &heldGate{passed: passed, started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (h *heldGate) Run(context.Context, string, string) (application.Checked, error) {
	h.mu.Lock()
	h.runs++
	h.mu.Unlock()
	h.started <- struct{}{}
	<-h.release
	out := "ok\n"
	if !h.passed {
		out = "--- FAIL: TestSomething\n"
	}
	return application.Checked{Command: "make test", Passed: h.passed, Output: out}, nil
}

func (h *heldGate) ranCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runs
}

// gateStarter stands in for the process mw starts a whole check in: it runs the
// same Check as a gate worker in a goroutine, which outlives the call that asked.
type gateStarter struct{ workerStarter }

func (s *gateStarter) Start(ctx context.Context, story, logPath string) (int, error) {
	return s.workerStarter.Start(ctx, story, 0, logPath)
}

func (s *gateStarter) wait() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

// aForegroundCheckOnAGateLongerThanTheCall is mw check with no --part, in a
// Builder's call of the default 540 s, of a rig whose gate usually takes 12m47s on
// this host: the call is the front, the process it starts is the same Check with
// GateWorker set, and the call's clock is moved by hand (mw-gq6.1273).
type aForegroundCheckOnAGateLongerThanTheCall struct {
	w       *aWholeCheckBesideTheHostsGateLock
	inCall  *gateTakingTheCall
	gate    *heldGate
	starter *gateStarter
	records *logRecords
	check   func() (application.CheckReport, error)
}

const cloud8sUsualGate = 12*time.Minute + 47*time.Second

func newForegroundCheckOnAGateLongerThanTheCall(t *testing.T, passed bool) *aForegroundCheckOnAGateLongerThanTheCall {
	t.Helper()
	w := newWholeCheckBesideTheGateLock(t, nil)
	f := &aForegroundCheckOnAGateLongerThanTheCall{
		w:       w,
		inCall:  &gateTakingTheCall{clock: w.clock, took: cloud8sUsualGate},
		gate:    newHeldGate(passed),
		starter: &gateStarter{},
		records: &logRecords{dir: t.TempDir()},
	}
	landing := aLandingAtATip{&fakeRetryLanding{AheadCount: 1}, "abc1234"}
	build := func(worker bool, out io.Writer) application.Check {
		k := application.Check{
			Tracker: aStoryOnTheLaptop(), Landing: landing,
			Slot: w.slots, GateLock: w.gate, Records: f.records, Logs: f.records,
			SpecsPoll: 5 * time.Millisecond, GateNoticeEvery: 10 * time.Millisecond,
			Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": w.rigDir}, Out: out,
		}
		if worker {
			// mw check <story> --gate-worker --wait 40m: not kept to a call.
			k.Checks, k.GateWorker = f.gate, true
			k.PartSlotWait, k.HandBackAtOnce = 40*time.Minute, true
		} else {
			k.Checks, k.GateStarter = f.inCall, f.starter
			k.PartSlotWait, k.KeepPlace = application.CheckPartSlotWait, application.CheckKeepPlace
			k.CallBudget, k.CallNow = application.CheckCallBudget, w.clock.Now
			k.GateExpected = func(context.Context, string) time.Duration { return cloud8sUsualGate }
		}
		return k
	}
	f.starter.run = func() {
		said := &lockedBuffer{}
		_, _ = build(true, writerOf(said)).Run(context.Background(), "mw-x.1")
		_, _ = f.records.PutRunFile(context.Background(), "mw-x.1", application.CheckGateOutFileName(1), said.String())
	}
	f.check = func() (application.CheckReport, error) {
		return build(false, writerOf(w.told)).Run(context.Background(), "mw-x.1")
	}
	return f
}

// A foreground whole check on a host whose usual gate is longer than the call's
// whole budget does not run the gate inside the call, which the tool cuts off at
// 600 s with no words, no log and the gate lock held (mw-gq6.1273, seen by
// mw-gq6.1251 on cloud8: "gate expected about 12m47s, 8m50s left in this call"). It
// runs the check as a process of its own that outlives the call, ends the call
// inside its budget in one line naming where the verdict and the output will be,
// and the same command, run again, reads the verdict without running the gate
// again; the log it keeps then is the passed check mw next trusts.
func TestAForegroundCheckOnAGateLongerThanTheCall(t *testing.T) {
	f := newForegroundCheckOnAGateLongerThanTheCall(t, true)
	began := f.w.clock.Now()
	// The whole of the call's budget passes while the gate runs.
	go func() {
		<-f.gate.started
		f.w.clock.Pass(application.CheckCallBudget)
	}()
	report, err := f.check()
	if spent := f.w.clock.Now().Sub(began); spent >= 590*time.Second {
		t.Fatalf("expected the call to end before 590 s, it ran %s with the gate run in it %d time(s) (%v)\n%s", spent, f.inCall.ranCount(), err, report)
	}
	if err != nil || len(report.Refusals) != 0 {
		t.Fatalf("expected a call that ended on a running gate to end without an error or a refusal, got %v %+v\n%s", err, report.Refusals, report)
	}
	if f.inCall.ranCount() != 0 || f.starter.startedCount() != 1 {
		t.Fatalf("expected the gate run by one process of its own and not in the call, ran %d in the call, %d process(es)", f.inCall.ranCount(), f.starter.startedCount())
	}
	for _, want := range []string{
		"still running on laptop as a process of its own", "usually about 12m47s",
		f.records.RunFile("mw-x.1", application.CheckGateResultFileName(1)),
		f.records.RunFile("mw-x.1", application.CheckGateOutFileName(1)),
		"`mw check mw-x.1`, run again, waits for it and reads the verdict without running the rig's gate again",
	} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if strings.Contains(report.Waiting, "\n") {
		t.Errorf("expected one line, got %q", report.Waiting)
	}
	if log, _ := f.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckLogFileName(1)); !strings.Contains(log, report.Waiting) {
		t.Errorf("expected the check's log to hold the line, got %q", log)
	}

	// The gate ends after the call; the same command, run again, reads it.
	close(f.gate.release)
	f.starter.wait()
	report, err = f.check()
	if err != nil || !report.Passed() || report.Tip != "abc1234" {
		t.Fatalf("expected the check run again to pass at abc1234 on what the process kept, got %v (tip %q)\n%s", err, report.Tip, report)
	}
	if f.inCall.ranCount() != 0 || f.gate.ranCount() != 1 || f.starter.startedCount() != 1 {
		t.Errorf("expected the gate run once, by the one process, got %d in the call, %d by %d process(es)", f.inCall.ranCount(), f.gate.ranCount(), f.starter.startedCount())
	}
	log, _ := f.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckLogFileName(1))
	if got := application.CheckedTip(log); got != "abc1234" {
		t.Errorf("expected the check's log to be the passed check at abc1234 that mw next trusts, got tip %q of %q", got, log)
	}
}

// A process that ran the gate and failed it gives the call run again its verdict:
// the tests' refusal, with nothing started or run again.
func TestAForegroundCheckOnAGateLongerThanTheCallReadsAFailedVerdict(t *testing.T) {
	f := newForegroundCheckOnAGateLongerThanTheCall(t, false)
	go func() {
		<-f.gate.started
		f.w.clock.Pass(application.CheckCallBudget)
	}()
	if report, err := f.check(); err != nil || report.Waiting == "" {
		t.Fatalf("expected the call to end on the running gate, got %v\n%s", err, report)
	}
	close(f.gate.release)
	f.starter.wait()
	report, err := f.check()
	if err == nil || len(report.Refusals) == 0 {
		t.Fatalf("expected the failed gate to be the call's refusal, got %v\n%s", err, report)
	}
	if !strings.Contains(report.String(), "--- FAIL: TestSomething") {
		t.Errorf("expected the refusal to say what the tests printed, got %s", report)
	}
	if f.inCall.ranCount() != 0 || f.gate.ranCount() != 1 || f.starter.startedCount() != 1 {
		t.Errorf("expected nothing run or started again, got %d in the call, %d by %d process(es)", f.inCall.ranCount(), f.gate.ranCount(), f.starter.startedCount())
	}
}
