package application_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// heldSpecs is the rig's changed-specs command, which the test holds: a run says
// it has started and stays until the test lets it go, passing or failing as the
// test says.
type heldSpecs struct {
	mu      sync.Mutex
	ran     int
	started chan struct{}
	release chan struct{}
	passed  bool
}

func newHeldSpecs(passed bool) *heldSpecs {
	return &heldSpecs{started: make(chan struct{}, 8), release: make(chan struct{}), passed: passed}
}

func (h *heldSpecs) RunSpecs(context.Context, string, string, string) (application.SpecsChecked, error) {
	h.mu.Lock()
	h.ran++
	h.mu.Unlock()
	h.started <- struct{}{}
	<-h.release
	return application.SpecsChecked{
		Checked: application.Checked{Command: "npx playwright test a.spec.ts", Passed: h.passed, Output: "the specs said this\n"},
		Specs:   []string{"a.spec.ts"},
	}, nil
}

func (h *heldSpecs) ranCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ran
}

// specsStarter stands in for the process mw starts the changed specs in: it runs
// the same Check as a specs worker in a goroutine, which outlives the call that
// asked, and says whether it is still running.
type specsStarter struct {
	workerStarter
}

func (s *specsStarter) Start(ctx context.Context, story, _ string) (int, error) {
	return s.workerStarter.Start(ctx, story, 0, "")
}

type detachedSpecsRig struct {
	specs   *heldSpecs
	checks  *slowChecks
	records *logRecords
	starter *specsStarter
	told    *lockedBuffer
	clock   *callClock
	check   func() (application.CheckReport, error)
}

// aWholeCheckWhoseSpecsOutrunTheCall is mw check with no --part of a rig whose
// changed specs take longer than the Builder's call: the call is the front, the
// process it starts for the specs is the same Check with SpecsWorker set, and the
// call's clock is moved by hand, never by how long the host took (mw-gq6.988).
func aWholeCheckWhoseSpecsOutrunTheCall(t *testing.T, passed bool) *detachedSpecsRig {
	t.Helper()
	w := newWholeCheckBesideTheGateLock(t, &slowChecks{pass: true})
	d := &detachedSpecsRig{
		specs:   newHeldSpecs(passed),
		checks:  &slowChecks{pass: true},
		records: &logRecords{dir: t.TempDir()},
		starter: &specsStarter{},
		told:    &lockedBuffer{},
		clock:   newCallClock(),
	}
	landing := aLandingAtATip{&fakeRetryLanding{AheadCount: 1}, "abc1234"}
	build := func(worker bool, out *lockedBuffer) application.Check {
		k := application.Check{
			Tracker: aStoryOnTheLaptop(), Landing: landing, Checks: d.checks, SpecChecks: d.specs,
			Records: d.records, Logs: d.records, SpecsPoll: 5 * time.Millisecond,
			GateNoticeEvery: 10 * time.Millisecond, Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(out),
		}
		if worker {
			k.SpecsWorker = true
		} else {
			k.SpecsStarter = d.starter
			k.CallBudget, k.CallNow, k.KeepPlace = application.CheckCallBudget, d.clock.Now, application.CheckKeepPlace
		}
		return k
	}
	d.starter.run = func() {
		said := &lockedBuffer{}
		_, _ = build(true, said).Run(context.Background(), "mw-x.1")
		_, _ = d.records.PutRunFile(context.Background(), "mw-x.1", application.CheckSpecsOutFileName(1), said.String())
	}
	d.check = func() (application.CheckReport, error) {
		return build(false, d.told).Run(context.Background(), "mw-x.1")
	}
	return d
}

// A whole check whose changed specs outrun the Builder's call does not fail and
// is not cut off with the call: the specs are run by a process of its own, and the
// call ends inside its budget in one line saying where the verdict will be and the
// one command that reads it. That command, run again, takes up the same run, does
// not run the rig's tests or the specs twice, and says what the specs said.
func TestAWholeChecksChangedSpecsThatOutrunTheCallRunOnAndTheSameCheckReadsTheVerdict(t *testing.T) {
	d := aWholeCheckWhoseSpecsOutrunTheCall(t, true)

	// The specs are still running when the call's 540 seconds are over.
	go func() {
		<-d.specs.started
		d.clock.Pass(application.CheckCallBudget + time.Minute)
	}()
	report, err := d.check()
	if err != nil {
		t.Fatalf("expected a call that ended on running specs to end without an error, got %v\n%s", err, report)
	}
	if len(report.Refusals) != 0 {
		t.Errorf("expected no refusal, specs still running are not a failure, got %+v", report.Refusals)
	}
	verdict := d.records.RunFile("mw-x.1", application.CheckSpecsResultFileName(1))
	for _, want := range []string{"changed specs are still running on laptop", verdict, "`mw check mw-x.1`, run again, reads it without running the rig's tests or the specs again"} {
		if !strings.Contains(report.Waiting, want) {
			t.Errorf("expected the line to say %q, got %q", want, report.Waiting)
		}
	}
	if strings.Contains(report.Waiting, "\n") {
		t.Errorf("expected one line, got %q", report.Waiting)
	}
	if !strings.Contains(d.told.String(), report.Waiting) || strings.Contains(d.told.String(), "passed") {
		t.Errorf("expected the line to be printed and no verdict, got %q", d.told.String())
	}

	// The second call finds the run and waits for it: the specs are let go while it
	// waits, and neither they nor the rig's tests are run again.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(d.specs.release)
	}()
	report, err = d.check()
	if err != nil || !report.Passed() {
		t.Fatalf("expected the second call to see the specs pass, got %v\n%s", err, report)
	}
	if d.specs.ranCount() != 1 || d.starter.startedCount() != 1 {
		t.Errorf("expected the specs to be run once by one process, ran %d time(s) in %d process(es)", d.specs.ranCount(), d.starter.startedCount())
	}
	if d.checks.runs != 1 {
		t.Errorf("expected the rig's tests to run once, they ran %d times", d.checks.runs)
	}

	// A third call at the same commit finds the pass kept and runs nothing.
	if report, err = d.check(); err != nil || !report.Passed() {
		t.Fatalf("expected the third call to pass on the kept result, got %v\n%s", err, report)
	}
	if d.specs.ranCount() != 1 || d.starter.startedCount() != 1 {
		t.Errorf("expected a pass kept for the commit to be used, specs ran %d time(s) in %d process(es)", d.specs.ranCount(), d.starter.startedCount())
	}
}

// Specs that fit the call behave as they do without a process of their own: the
// one call says what they said, in the words of the check that ran them itself.
func TestAWholeChecksChangedSpecsThatFitTheCallAreToldInThatCall(t *testing.T) {
	d := aWholeCheckWhoseSpecsOutrunTheCall(t, true)
	close(d.specs.release)

	report, err := d.check()
	if err != nil || !report.Passed() || report.Waiting != "" {
		t.Fatalf("expected the check to pass in one call, got %v\n%s", err, report)
	}
}

func TestAWholeChecksChangedSpecsThatFailAfterTheCallAreRefusedWithWhatTheySaid(t *testing.T) {
	d := aWholeCheckWhoseSpecsOutrunTheCall(t, false)
	close(d.specs.release)

	report, err := d.check()
	if err == nil || len(report.Refusals) != 1 {
		t.Fatalf("expected the failing specs to be refused, got %v\n%s", err, report)
	}
	why := report.Refusals[0].Why
	for _, want := range []string{"changed specs fail", "npx playwright test a.spec.ts", "a.spec.ts"} {
		if !strings.Contains(why, want) {
			t.Errorf("expected the refusal to say %q, got %q", want, why)
		}
	}
	if !strings.Contains(report.Refusals[0].Said, "the specs said this") {
		t.Errorf("expected what the specs said to be shown, got %q", report.Refusals[0].Said)
	}
}

// A run whose process died before it kept a result is not waited for again and
// again: the next call says so and starts the specs afresh.
func TestChangedSpecsWhoseProcessDiedWithoutAResultAreStartedAgain(t *testing.T) {
	d := aWholeCheckWhoseSpecsOutrunTheCall(t, true)
	worker := d.starter.run
	d.starter.run = func() {}
	report, err := d.check()
	if err == nil || len(report.Refusals) != 1 || !strings.Contains(report.Refusals[0].Why, "ended without") {
		t.Fatalf("expected the call to say the run ended without a result, got %v\n%s", err, report)
	}
	d.starter.run = worker
	close(d.specs.release)
	if report, err = d.check(); err != nil || !report.Passed() {
		t.Fatalf("expected the specs to be started afresh and pass, got %v\n%s", err, report)
	}
	if d.starter.startedCount() != 2 {
		t.Errorf("expected the specs to be started again, they were started %d time(s)", d.starter.startedCount())
	}
}

// A check that is not asked to keep to a call (--background, --wait) runs the
// specs itself, as it always did.
func TestAWholeCheckNotKeptToACallRunsItsSpecsItself(t *testing.T) {
	d := aWholeCheckWhoseSpecsOutrunTheCall(t, true)
	close(d.specs.release)
	w := newWholeCheckBesideTheGateLock(t, d.checks)
	report, err := application.Check{
		Tracker: aStoryOnTheLaptop(), Landing: aLandingAtATip{&fakeRetryLanding{AheadCount: 1}, "abc1234"}, Checks: d.checks, SpecChecks: d.specs,
		Records: d.records, Logs: d.records, SpecsStarter: d.starter, Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(d.told),
	}.Run(context.Background(), "mw-x.1")
	if err != nil || !report.Passed() {
		t.Fatalf("expected the check to pass, got %v\n%s", err, report)
	}
	if d.starter.startedCount() != 0 || d.specs.ranCount() != 1 {
		t.Errorf("expected no process of its own, started %d, specs ran %d time(s)", d.starter.startedCount(), d.specs.ranCount())
	}
}
