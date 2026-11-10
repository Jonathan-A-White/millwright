package application_test

import (
	"context"
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

// workerParts is a rig's gate of one part whose command the test holds: a run
// says it has started and stays until the test lets it go, passing or failing as
// the test says.
type workerParts struct {
	mu      sync.Mutex
	ran     int
	started chan struct{}
	release chan struct{}
	passed  bool
}

func newWorkerParts(passed bool) *workerParts {
	return &workerParts{started: make(chan struct{}, 8), release: make(chan struct{}), passed: passed}
}

func (w *workerParts) Parts(string) []application.GatePart {
	return []application.GatePart{{Name: "vitest 5/6", Command: "npx vitest run --shard=5/6"}}
}

func (w *workerParts) RunLine(_ context.Context, _, _, command string) (application.Checked, error) {
	w.mu.Lock()
	w.ran++
	w.mu.Unlock()
	w.started <- struct{}{}
	<-w.release
	return application.Checked{Command: command, Passed: w.passed, Output: "the part said this"}, nil
}

func (w *workerParts) ranCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ran
}

// logRecords is the vault's run files with the path a log is kept at.
type logRecords struct {
	mapRecords
	dir string
}

func (l *logRecords) RunFile(story, name string) string { return filepath.Join(l.dir, story, name) }

// workerStarter stands in for the process mw starts the part in: it runs the same
// Check as a part worker in a goroutine, which outlives the call that asked, and
// says whether it is still running.
type workerStarter struct {
	mu      sync.Mutex
	started int
	done    chan struct{}
	run     func()
}

func (s *workerStarter) Start(_ context.Context, _ string, _ int, _ string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started++
	s.done = make(chan struct{})
	done := s.done
	go func() {
		defer close(done)
		s.run()
	}()
	return 4242, nil
}

func (s *workerStarter) Alive(int) bool {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

func (s *workerStarter) startedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

type detachedPartRig struct {
	parts   *workerParts
	records *logRecords
	starter *workerStarter
	told    *lockedBuffer
	rigDir  string
	load    *flippingLoad
	check   func(budget time.Duration) error
}

// flippingLoad is a host that reads calm until the test sets it far over its cores.
type flippingLoad struct {
	mu   sync.Mutex
	over bool
}

func (f *flippingLoad) set(over bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.over = over
}

func (f *flippingLoad) Load(context.Context) (application.LoadReading, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.over {
		return application.LoadReading{Load: 45, Cores: 20}, nil
	}
	return application.LoadReading{Load: 3, Cores: 20}, nil
}

// aDetachedPartRig is mw check --part 1 of a gate whose part outruns the
// Builder's call: the front is run with a budget, and the worker it starts is the
// same Check with PartWorker set (mw-gq6.1029).
func aDetachedPartRig(t *testing.T, passed bool) *detachedPartRig {
	t.Helper()
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := &detachedPartRig{
		parts:   newWorkerParts(passed),
		records: &logRecords{dir: t.TempDir()},
		starter: &workerStarter{},
		told:    &lockedBuffer{},
		rigDir:  rigDir,
		load:    &flippingLoad{},
	}
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	build := func(worker bool, budget time.Duration, out *lockedBuffer) application.Check {
		k := application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
			Parts: d.parts, Records: d.records, Logs: d.records, Part: 1,
			KeepPlace: 10 * time.Minute, CallBudget: budget,
			GateNoticeEvery: 10 * time.Millisecond, PartPoll: 5 * time.Millisecond,
			Host: "laptop", Remote: "origin",
			Load: d.load, LoadPoll: 5 * time.Millisecond, PartSlotWait: 40 * time.Millisecond,
			Rigs: map[string]string{"millwright": rigDir},
			Out:  writerOf(out),
		}
		if worker {
			k.PartWorker = true
		} else {
			k.PartStarter = d.starter
		}
		return k
	}
	d.starter.run = func() {
		said := &lockedBuffer{}
		_, _ = build(true, 0, said).Run(context.Background(), "mw-x.1")
		// The process's output is the file its starter was given.
		_, _ = d.records.PutRunFile(context.Background(), "mw-x.1", application.CheckPartOutFileName(1, 1), said.String())
	}
	d.check = func(budget time.Duration) error {
		_, err := build(false, budget, d.told).Run(context.Background(), "mw-x.1")
		return err
	}
	return d
}

// A part that outruns the Builder's call does not fail and is not cut off with
// the call: it is run by a process of its own, and the call says so in one line
// naming the one command that waits for it. That command, run again, takes up the
// same run and does not start the part twice; when the part is done it says what
// the part said (mw-gq6.1029).
func TestAPartThatOutrunsTheCallRunsOnAndTheSameCommandWaitsForIt(t *testing.T) {
	d := aDetachedPartRig(t, true)

	err := d.check(150 * time.Millisecond)
	if err == nil {
		t.Fatal("expected a call that ended before the part did to say so, not to pass")
	}
	for _, want := range []string{"part 1 of 1 (vitest 5/6) is still running", "mw check mw-x.1 --part 1", "wait"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the line to say %q, got %q", want, err.Error())
		}
	}
	if strings.Contains(err.Error(), "did not start") {
		t.Errorf("expected a part that is running not to be said to have failed to start, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("expected one line, got %q", err.Error())
	}
	select {
	case <-d.parts.started:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the part to be running on its own")
	}

	// The second call finds the run and waits for it: the part is let go while it
	// waits, and it is not started again.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(d.parts.release)
	}()
	if err := d.check(30 * time.Second); err != nil {
		t.Fatalf("expected the second call to see the part pass, got %v", err)
	}
	if d.parts.ranCount() != 1 || d.starter.startedCount() != 1 {
		t.Errorf("expected the part to be run once by one process, ran %d time(s) in %d process(es)", d.parts.ranCount(), d.starter.startedCount())
	}
	if !strings.Contains(d.told.String(), "part 1 of 1 (vitest 5/6) passed") {
		t.Errorf("expected the part's own words once it was done, got %q", d.told.String())
	}
	text, err := d.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 1))
	if err != nil || !strings.Contains(text, "passed") {
		t.Errorf("expected the part's record to say it passed, got %q, %v", text, err)
	}
}

// A detached part that fails is told as a foreground one is: it did not pass, with
// what it said.
func TestADetachedPartThatFailsIsToldToTheCallThatWaitedForIt(t *testing.T) {
	d := aDetachedPartRig(t, false)
	close(d.parts.release)

	err := d.check(30 * time.Second)
	if err == nil || !strings.Contains(err.Error(), "part 1 of 1 (vitest 5/6) did not pass") {
		t.Fatalf("expected the part to be said not to pass, got %v", err)
	}
	if !strings.Contains(d.told.String(), "the part said this") {
		t.Errorf("expected what the part said to be shown, got %q", d.told.String())
	}
}

// A run whose process died before it kept a result is not waited for again and
// again: the next call says so and starts the part afresh.
func TestADetachedPartWhoseProcessDiedWithoutAResultIsStartedAgain(t *testing.T) {
	d := aDetachedPartRig(t, true)
	worker := d.starter.run
	// The process "dies" at once, writing nothing.
	d.starter.run = func() {}
	err := d.check(30 * time.Second)
	if err == nil || !strings.Contains(err.Error(), "ended without") {
		t.Fatalf("expected the call to say the run ended without a result, got %v", err)
	}
	d.starter.run = worker
	close(d.parts.release)
	if err := d.check(30 * time.Second); err != nil {
		t.Fatalf("expected the part to be started afresh and pass, got %v", err)
	}
	if d.starter.startedCount() != 2 {
		t.Errorf("expected the part to be started again, it was started %d time(s)", d.starter.startedCount())
	}
}

// A part run by its own process that failed while the host was far over its cores,
// and is handed back on the wait for the run again, keeps a record of the hand-back
// in place of "running": the next call says so, and the first failure is not
// counted as the part's result (mw-gq6.1065).
func TestADetachedPartHandedBackOnTheRunAgainWaitIsToldAsHandedBackNotAsStopped(t *testing.T) {
	d := aDetachedPartRig(t, false)

	if err := d.check(100 * time.Millisecond); err == nil || !strings.Contains(err.Error(), "is still running") {
		t.Fatalf("expected the first call to leave the part running, got %v", err)
	}
	<-d.parts.started
	// The host is far over its cores when the part fails, and stays so.
	d.load.set(true)
	close(d.parts.release)

	err := d.check(30 * time.Second)
	if err == nil {
		t.Fatal("expected the hand-back to be told, not a pass")
	}
	for _, want := range []string{"failed once while laptop was busy", "mw check mw-x.1 --part 1", "not counted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the line to say %q, got %q", want, err.Error())
		}
	}
	if strings.Contains(err.Error(), "was stopped") || strings.Contains(err.Error(), "did not pass") {
		t.Errorf("expected a hand-back, not a stopped process or a failure, got %q", err.Error())
	}
	text, _ := d.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 1))
	if lines := strings.Split(text, "\n"); len(lines) < 2 || lines[1] == "running" || lines[1] == "failed" || lines[1] == "passed" {
		t.Errorf("expected the record to say it was handed back, neither running nor a result, got %q", text)
	}
}
