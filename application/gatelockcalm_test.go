package application_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1420: a lampas batch took over 4 hours to pass its gate on the Laptop.
// Every holder of the host's gate lock kept it 15 to 37 minutes, because the wait
// for a calm host was spent holding it (the batch's log: 'waiting up to 15m0s for
// it to calm', with the lock held; runs/mw-crygrun/check-1.log the same at load
// 24 of 20), and the batch queued again behind every waiter after each of its 11
// parts. No holder keeps the lock while it only waits for the host to calm, and a
// batch's turns are bounded.

// aWatchedGateLock is the host's gate lock as a test watches it: whether it is
// held now, and whether it was ever taken while held.
type aWatchedGateLock struct {
	mu     sync.Mutex
	held   bool
	twice  bool
	taken  int
	holder string
	// clock, when it is set, is moved by wait at every take after the first: the
	// round of the other gates queued for the lock.
	clock *time.Time
	wait  time.Duration
	// ahead counts the takes asked ahead of single stories (WithQueuePrecedence).
	ahead int
}

func (g *aWatchedGateLock) Take(ctx context.Context, holder string, held func(string)) (application.GateTurn, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held {
		g.twice = true
	}
	g.held, g.holder = true, holder
	g.taken++
	if application.HasQueuePrecedence(ctx) {
		g.ahead++
	}
	if g.clock != nil && g.taken > 1 && g.wait > 0 {
		if held != nil {
			held("four other gates")
		}
		*g.clock = g.clock.Add(g.wait)
		return application.GateTurn{Holding: aWatchedHolding{g}, Waited: g.wait, For: "four other gates"}, nil
	}
	return application.GateTurn{Holding: aWatchedHolding{g}}, nil
}

func (g *aWatchedGateLock) isHeld() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held
}

type aWatchedHolding struct{ g *aWatchedGateLock }

func (h aWatchedHolding) Release(context.Context) error {
	h.g.mu.Lock()
	defer h.g.mu.Unlock()
	h.g.held = false
	return nil
}

func (h aWatchedHolding) HeldBy() string { return h.g.holder }

// aCalmWatch is the wait for a calm host as a test watches it: how many steps
// were waited, and how many of them with the gate lock held.
type aCalmWatch struct {
	lock      *aWatchedGateLock
	waits     int
	heldWaits int
	// then is called after each step, as a host that calms does.
	then func(time.Duration)
}

func (w *aCalmWatch) wait(_ context.Context, d time.Duration) error {
	w.waits++
	if w.lock.isHeld() {
		w.heldWaits++
	}
	if w.then != nil {
		w.then(d)
	}
	return nil
}

// sawTheLockFree fails the test when the wait for a calm host was not made, or
// was made with the gate lock held.
func (w *aCalmWatch) sawTheLockFree(t *testing.T, holder string) {
	t.Helper()
	if w.waits == 0 {
		t.Fatalf("expected %s to wait for the host to calm", holder)
	}
	if w.heldWaits > 0 {
		t.Errorf("expected %s to hold no gate lock while it waits for the host to calm: %d of its %d steps of the wait were made holding it", holder, w.heldWaits, w.waits)
	}
	if w.lock.twice {
		t.Errorf("expected %s never to take the gate lock while it held it", holder)
	}
	if w.lock.isHeld() {
		t.Errorf("expected %s to give the gate lock back at the end", holder)
	}
}

// checksUnderTheLock are the rig's tests, which note whether the gate lock was
// held when they ran.
type checksUnderTheLock struct {
	lock     *aWatchedGateLock
	runs     int
	unlocked int
	fail     bool
}

func (c *checksUnderTheLock) Run(context.Context, string, string) (application.Checked, error) {
	c.runs++
	if !c.lock.isHeld() {
		c.unlocked++
	}
	if c.fail {
		return application.Checked{Command: "make test", Output: "--- FAIL: TestA (0.01s)\nFAIL\n"}, nil
	}
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

func (c *checksUnderTheLock) ranUnderTheLock(t *testing.T) {
	t.Helper()
	if c.runs == 0 {
		t.Fatal("expected the rig's tests to run")
	}
	if c.unlocked > 0 {
		t.Errorf("expected the rig's tests to run holding the gate lock, %d of %d runs did not", c.unlocked, c.runs)
	}
}

// busyThenCalm is a host over its cores for the first reads and calm after.
func busyThenCalm(busyReads int) *apptest.FakeHostLoad {
	var readings []application.LoadReading
	for range busyReads {
		readings = append(readings, halfOver())
	}
	return &apptest.FakeHostLoad{Readings: append(readings, calm())}
}

func TestABatchsGateHoldsNoGateLockWhileItWaitsForTheHostToCalm(t *testing.T) {
	lock := &aWatchedGateLock{}
	watch := &aCalmWatch{lock: lock}
	checks := &checksUnderTheLock{lock: lock}
	var out bytes.Buffer
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: newFakeVault(), Checks: checks, GateLock: lock,
		Load: busyThenCalm(3), LoadWait: watch.wait, LoadPoll: time.Minute, LoadBound: 15 * time.Minute,
		Host: "laptop", Seat: "builder", Out: &out, Err: &out,
	}}
	report := &application.BatchReport{Rig: "lampas", Number: 552, Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}}}

	checked, err, _ := land.GateTheBatchRun(context.Background(), t.TempDir(), report)

	if err != nil || !checked.Passed {
		t.Fatalf("expected the batch's gate to pass once the host calmed, got %+v and %v\n%s", checked, err, out.String())
	}
	watch.sawTheLockFree(t, "a batch's gate")
	checks.ranUnderTheLock(t)
	// The batch asks for the lock again after the wait ahead of single stories'
	// checks and close-outs, as it asked for it first (mw-gq6.1450).
	if lock.taken < 2 || lock.ahead != lock.taken {
		t.Errorf("expected every take of the batch's gate lock, the one after the wait for a calm host too, asked ahead of single stories: %d of %d were", lock.ahead, lock.taken)
	}
	if line := lineWith(out.String(), "while it waits for the host to calm"); !strings.Contains(line, "takes it again ahead of single stories' checks and close-outs") {
		t.Errorf("expected the batch to say it takes the lock again ahead of single stories, got %q\n%s", line, out.String())
	}
}

// aCloseOutWatched is mw next's close-out of one story under the watched lock, on
// a host whose load is load.
func aCloseOutWatched(t *testing.T, lock *aWatchedGateLock, watch *aCalmWatch, checks application.Checks, load application.HostLoad, named application.NamedChecks) (application.NextReport, error) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	return application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: checks, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(), GateLock: lock,
		NamedChecks: named, Load: load, LoadWait: watch.wait, LoadPoll: time.Minute,
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
}

func TestACloseOutsGateHoldsNoGateLockWhileItWaitsForTheHostToCalm(t *testing.T) {
	lock := &aWatchedGateLock{}
	watch := &aCalmWatch{lock: lock}
	checks := &checksUnderTheLock{lock: lock}

	report, err := aCloseOutWatched(t, lock, watch, checks, busyThenCalm(3), nil)

	if err != nil || !report.Landed {
		t.Fatalf("expected the story to land once the host calmed, got %v and %+v", err, report)
	}
	watch.sawTheLockFree(t, "a close-out's gate")
	checks.ranUnderTheLock(t)
}

// aHostThatFillsAfterTheGate is a host that is calm until the gate's own run ends
// and over its cores after it, until a wait for it to calm: what a close-out's run
// of the failing tests on main alone finds (mw-gq6.1085).
type aHostThatFillsAfterTheGate struct {
	mu   sync.Mutex
	full bool
}

func (h *aHostThatFillsAfterTheGate) Load(context.Context) (application.LoadReading, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.full {
		return application.LoadReading{Load: 10, Cores: 8}, nil
	}
	return application.LoadReading{Load: 2, Cores: 8}, nil
}

func (h *aHostThatFillsAfterTheGate) set(full bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.full = full
}

func TestTheRunOnMainAloneHoldsNoGateLockWhileItWaitsForRoom(t *testing.T) {
	lock := &aWatchedGateLock{}
	host := &aHostThatFillsAfterTheGate{}
	watch := &aCalmWatch{lock: lock, then: func(time.Duration) { host.set(false) }}
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return passedOnMain(), nil
	}}

	report, _ := aCloseOutWatched(t, lock, watch, twoFailingTests{afterwards: func() { host.set(true) }}, host, named)

	if report.Reason != application.ReasonTestsFail || len(named.asked) != 1 {
		t.Fatalf("expected a tests-fail stop with one run on main alone, got %q and %d runs", report.Reason, len(named.asked))
	}
	watch.sawTheLockFree(t, "the run on main alone")
}

func TestTheRunOnMainAloneHoldsNoGateLockWhileItWaitsToRunAgain(t *testing.T) {
	lock := &aWatchedGateLock{}
	h := &aHostBusyDuringTheRunOnMain{
		fills:   []float64{20},
		results: []application.NamedRun{failedOnMain(bothFailOnMain), passedOnMain()},
	}
	watch := &aCalmWatch{lock: lock, then: func(d time.Duration) { _ = h.wait(context.Background(), d) }}

	aCloseOutWatched(t, lock, watch, twoFailingTests{}, h, h)

	if len(h.asked) != 2 {
		t.Fatalf("expected the run on main to be made twice, got %d", len(h.asked))
	}
	watch.sawTheLockFree(t, "the run on main alone, before its run again")
}

func TestAWholeCheckHoldsNoGateLockWhileItWaitsForTheHostToCalm(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := &aWatchedGateLock{}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	watch := &aCalmWatch{lock: lock, then: func(d time.Duration) { now = now.Add(d) }}
	checks := &checksUnderTheLock{lock: lock}
	var said strings.Builder

	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: aSlot{}, GateLock: lock, Records: &mapRecords{}, PartSlotWait: 9 * time.Minute,
		CallNow: func() time.Time { return now },
		Load:    busyThenCalm(3), LoadPoll: 30 * time.Second, LoadWait: watch.wait,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": rigDir},
		Out:  &said,
	}.Run(context.Background(), "mw-x.1")

	if err != nil || !report.Passed() {
		t.Fatalf("expected the check to pass once the host calmed, got %v and %+v\n%s", err, report.Refusals, said.String())
	}
	watch.sawTheLockFree(t, "a whole check")
	checks.ranUnderTheLock(t)
}

func TestAPartOfACheckHoldsNoGateLockWhileItWaitsForTheHostToCalm(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := &aWatchedGateLock{}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	watch := &aCalmWatch{lock: lock, then: func(d time.Duration) { now = now.Add(d) }}
	parts := &toolParts{}

	_, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		Slot: aSlot{}, GateLock: lock, Parts: parts, Records: &mapRecords{}, Part: 2, PartSlotWait: 9 * time.Minute,
		CallNow: func() time.Time { return now },
		Load:    busyThenCalm(3), LoadPoll: 30 * time.Second, LoadWait: watch.wait,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": rigDir},
		Out:  io.Discard,
	}.Run(context.Background(), "mw-x.1")

	if err != nil || len(parts.ran) != 1 {
		t.Fatalf("expected part 2 to run once the host calmed, got %v and %q", err, parts.ran)
	}
	watch.sawTheLockFree(t, "a part of a check")
}

func TestTheSuiteHoldsNoGateLockWhileItWaitsForTheHostToCalm(t *testing.T) {
	lock := &aWatchedGateLock{}
	watch := &aCalmWatch{lock: lock}
	heads := apptest.NewFakeRigHeads()
	heads.SetHead("/rigs/lampas", "11541bc0d")
	ran := 0
	runner := &apptest.FakeSuiteRunner{Respond: func(string, string) (application.Checked, error) {
		ran++
		if !lock.isHeld() {
			t.Errorf("expected the suite's line to run holding the gate lock")
		}
		if ran == 1 {
			return application.Checked{Command: "npm test", Output: "FAIL tests/a.test.ts > a\n"}, nil
		}
		return application.Checked{Command: "npm test", Passed: true}, nil
	}}
	// Busy before the suite, calm for its run, far over once it failed, calm again.
	load := &apptest.FakeHostLoad{Readings: []application.LoadReading{halfOver(), calm(), farOver(), farOver(), calm()}}

	_, err := application.Suite{
		Trees: &apptest.FakeSuiteTrees{}, Heads: heads, Runner: runner, Records: &apptest.FakeSuiteRecords{},
		GateLock: lock, Load: load, LoadPoll: time.Minute, LoadWait: watch.wait,
		Now:   func() time.Time { return time.Date(2026, 11, 17, 8, 0, 1, 0, time.UTC) },
		Host:  "laptop",
		Rigs:  map[string]string{"lampas": "/rigs/lampas"},
		Tests: map[string]string{"lampas": "npm test"},
		Out:   io.Discard,
	}.Run(context.Background(), "lampas")

	if err != nil || ran != 2 {
		t.Fatalf("expected the suite to run its line again once the host calmed, got %d runs and %v", ran, err)
	}
	watch.sawTheLockFree(t, "the suite")
}

func TestTheTimingRunHoldsNoGateLockWhileItWaitsToRunAgain(t *testing.T) {
	f := newTimingNight(t)
	lock := &aWatchedGateLock{}
	watch := &aCalmWatch{lock: lock}
	f.night.GateLock = lock
	f.night.LoadWait = watch.wait
	f.failFirstThenPass(
		application.LoadReading{Load: 6.0, Cores: 4}, // busy before the run
		application.LoadReading{Load: 0.2, Cores: 4},
		application.LoadReading{Load: 9.0, Cores: 4}, // far over, after the failed run
		application.LoadReading{Load: 9.0, Cores: 4}, // still far over when the wait begins
		application.LoadReading{Load: 1.0, Cores: 4}, // calm on the wait
	)

	if err := f.night.Job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := f.runner.Ran(); len(got) != 2 {
		t.Fatalf("ran %d times, want 2: %+v\n%s", len(got), got, f.out.String())
	}
	watch.sawTheLockFree(t, "the timing run")
}

// elevenParts is lampas's gate as the Laptop names it: eleven parts, each run on
// a clock moved by part.
type elevenParts struct {
	clock *time.Time
	part  time.Duration
	lock  *aWatchedGateLock
	ran   []string
}

func (p *elevenParts) Parts(rig string) []application.GatePart {
	var parts []application.GatePart
	for i := 1; i <= 11; i++ {
		parts = append(parts, application.GatePart{Name: fmt.Sprintf("part %d", i), Command: fmt.Sprintf("npm run part-%d", i)})
	}
	return parts
}

func (p *elevenParts) RunLine(_ context.Context, _, _, command string) (application.Checked, error) {
	*p.clock = p.clock.Add(p.part)
	p.ran = append(p.ran, command)
	return application.Checked{Command: command, Passed: true, Output: command + ": passed\n"}, nil
}

func (p *elevenParts) Run(context.Context, string, string) (application.Checked, error) {
	return application.Checked{Command: "npm test", Passed: true}, nil
}

// anElevenPartBatch is batch 552 of lampas with four other gates that queue for
// the lock again for ever: every take after the first waits a round of them.
func anElevenPartBatch(t *testing.T, round time.Duration, load application.HostLoad, watch func(*aWatchedGateLock) func(context.Context, time.Duration) error) (gate application.Checked, err error, log string, parts *elevenParts, lock *aWatchedGateLock) {
	t.Helper()
	clock := time.Date(2026, 11, 17, 7, 59, 0, 0, time.UTC)
	lock = &aWatchedGateLock{clock: &clock, wait: round}
	parts = &elevenParts{clock: &clock, part: 4 * time.Minute, lock: lock}
	var out bytes.Buffer
	next := application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: newFakeVault(), Checks: parts, GateParts: parts, GateLock: lock,
		GateTurnsLimit: 30 * time.Minute, Load: load, LoadPoll: time.Minute, LoadBound: 15 * time.Minute,
		Now:  func() time.Time { return clock },
		Host: "laptop", Seat: "builder", Out: &out, Err: &out,
	}
	if watch != nil {
		next.LoadWait = watch(lock)
	} else {
		next.LoadWait = func(_ context.Context, d time.Duration) error { clock = clock.Add(d); return nil }
	}
	land := application.BatchLand{Next: next}
	report := &application.BatchReport{Rig: "lampas", Number: 552, Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}}}
	gate, err, _ = land.GateTheBatchRun(context.Background(), t.TempDir(), report)
	return gate, err, out.String(), parts, lock
}

// A batch takes precedence over single stories' gates and keeps the lock across
// its parts (mw-gq6.1450): behind gates that queue for ever it takes the lock once,
// waits for none of them between its parts, and says so once.
func TestAnElevenPartBatchBehindGatesThatQueueForEverKeepsTheGateLockAcrossItsParts(t *testing.T) {
	const round = 20 * time.Minute
	gate, err, log, parts, lock := anElevenPartBatch(t, round, nil, nil)

	if err != nil || !gate.Passed || len(parts.ran) != 11 {
		t.Fatalf("expected the batch's gate to pass by its 11 parts, got %d parts, %+v and %v\n%s", len(parts.ran), gate, err, log)
	}
	if lock.taken != 1 || strings.Contains(log, "takes it again behind whoever waits for it") {
		t.Errorf("expected the batch to take the gate lock once and keep it across its parts, it took it %d times\n%s", lock.taken, log)
	}
	var kept []string
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "  turn    ") && strings.Contains(line, "keeps laptop's gate lock") {
			kept = append(kept, line)
		}
	}
	if len(kept) != 1 {
		t.Fatalf("expected one 'turn' line saying the batch keeps the lock across its parts, got %q\n%s", kept, log)
	}
	for _, want := range []string{"batch 552 of lampas", "across its 11 parts", "ahead of single stories"} {
		if !strings.Contains(kept[0], want) {
			t.Errorf("expected the turn line to say %q, got %q", want, kept[0])
		}
	}
}

// A single story's close-out gate still takes turns between its parts (mw-gq6.1301),
// and its turns are bounded (mw-gq6.1420): behind gates that queue for ever it
// keeps the lock for its remaining parts once its turns have waited past the limit.
func TestAnElevenPartStoryGateBehindGatesThatQueueForEverEndsItsTurnsWithinTheLimitAndOneRound(t *testing.T) {
	const round = 20 * time.Minute
	clock := time.Date(2026, 11, 17, 7, 59, 0, 0, time.UTC)
	lock := &aWatchedGateLock{clock: &clock, wait: round}
	parts := &elevenParts{clock: &clock, part: 4 * time.Minute, lock: lock}
	var out lockedBuffer
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("lp-1", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("lp-1", domain.Story{ID: "lp-1.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["lp-1.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	_, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: parts, GateParts: parts, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(), GateLock: lock,
		GateTurnsLimit: 30 * time.Minute, Now: func() time.Time { return clock },
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"lampas": t.TempDir()},
		Out:  writerOf(&out), Err: writerOf(&out),
	}.Run(context.Background(), "lp-1.1")
	log := out.String()
	if err != nil || len(parts.ran) < 11 {
		t.Fatalf("expected the story's gate to pass by its 11 parts, got %d parts and %v\n%s", len(parts.ran), err, log)
	}
	// Each take again waited a round; the turns stop once they waited over the limit.
	taken := strings.Count(log, "takes it again behind whoever waits for it")
	if taken == 0 {
		t.Fatalf("expected the story's gate to take turns between its parts\n%s", log)
	}
	if waited, limit := time.Duration(taken)*round, 30*time.Minute+round; waited >= limit {
		t.Errorf("expected the story's waits for other gates to stay under the limit and one round, %s, it waited %s over %d turns\n%s", limit, waited, taken, log)
	}
	var kept []string
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "  turn    ") && strings.Contains(line, "keeps laptop's gate lock") {
			kept = append(kept, line)
		}
	}
	if len(kept) != 1 {
		t.Fatalf("expected one 'turn' line saying the story's gate keeps the lock for its remaining parts, got %q\n%s", kept, log)
	}
	for _, want := range []string{"30m", "remaining"} {
		if !strings.Contains(kept[0], want) {
			t.Errorf("expected the turn line to say %q, got %q", want, kept[0])
		}
	}
}

func TestTheLineThatEndsABatchsGateSaysHowLongItRanAndWaited(t *testing.T) {
	const round = 20 * time.Minute
	// The host is busy for two steps of the wait before the gate, and calm after.
	gate, err, log, _, _ := anElevenPartBatch(t, round, busyThenCalm(3), nil)

	if err != nil || !gate.Passed {
		t.Fatalf("expected the batch's gate to pass, got %+v and %v\n%s", gate, err, log)
	}
	line := lineWith(log, "the gate of batch 552 of lampas ended")
	if line == "" {
		t.Fatalf("expected mw land's log to say how the batch's gate ended, got:\n%s", log)
	}
	t.Logf("the line: %s", line)
	for _, want := range []string{application.BatchLogPrefix, "passed", "ran ", "waited ", "for other gates", "3m for laptop to calm"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to say %q, got %q", want, line)
		}
	}
}
