package application_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A scriptedChecks is the rig's tests as a host under load runs them: each run
// says what the next of its results is, and the last is said again.
type scriptedChecks struct {
	passes []bool
	runs   int
}

func (s *scriptedChecks) Run(context.Context, string, string) (application.Checked, error) {
	pass := s.passes[min(s.runs, len(s.passes)-1)]
	s.runs++
	return application.Checked{Command: "make test", Passed: pass, Output: fmt.Sprintf("run %d\n", s.runs)}, nil
}

// aSlot is a merge slot that is always free.
type aSlot struct{}

func (aSlot) Take(context.Context, string, string) (application.Holding, error) {
	return aHolding{}, nil
}

type aHolding struct{}

func (aHolding) Release(context.Context) error { return nil }
func (aHolding) HeldBy() string                { return "" }

// aLandingLanding is a landing that fast-forwards, so that only the rig's tests
// in the worktree are asked.
type aLandingLanding struct{ fakeRetryLanding }

func (*aLandingLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{Commit: "0123456789abcdef", FastForward: true}, nil
}

func busy() application.LoadReading { return application.LoadReading{Load: 39, Cores: 20} }
func calm() application.LoadReading { return application.LoadReading{Load: 3, Cores: 20} }

type gateRun struct {
	report  application.NextReport
	err     error
	vault   *fakeVault
	waits   int
	out     *strings.Builder
	mailbox *apptest.FakeMailbox
}

// aCloseOutWhoseTestsGo closes out a finished story whose tests give passes, on
// a host whose load reads as readings.
func aCloseOutWhoseTestsGo(t *testing.T, passes []bool, readings []application.LoadReading, bound time.Duration) (*gateRun, *scriptedChecks) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	checks := &scriptedChecks{passes: passes}
	run := &gateRun{vault: vault, out: &strings.Builder{}, mailbox: apptest.NewFakeMailbox()}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checks, Slot: aSlot{}, Mailbox: run.mailbox,
		Load:      &apptest.FakeHostLoad{Readings: readings},
		LoadPoll:  30 * time.Second,
		LoadBound: bound,
		LoadWait:  func(context.Context, time.Duration) error { run.waits++; return nil },
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  run.out, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	return run, checks
}

func TestAGateThatFailsOnABusyHostRunsOnceMoreWhenItCalmsAndLandsWhenThatPasses(t *testing.T) {
	// Read before the gate: calm. The gate fails. Read after it: busy. Read
	// before the second run: calm.
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false, true}, []application.LoadReading{calm(), busy(), calm()}, 15*time.Minute)

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed on the second run, got err %v and %+v", run.err, run.report)
	}
	if checks.runs != 2 {
		t.Fatalf("expected the gate run twice, got %d", checks.runs)
	}
	if len(run.vault.ledger) != 1 {
		t.Fatalf("expected one ledger line, got %q", run.vault.ledger)
	}
	for _, want := range []string{"ran twice", "first run failed", "load 39.0 of 20 cores", "second run passed"} {
		if !strings.Contains(run.vault.ledger[0], want) {
			t.Errorf("expected the ledger line to say %q, got %q", want, run.vault.ledger[0])
		}
	}
	if mail, _ := run.mailbox.Inbox(context.Background(), application.MayorMailbox); len(mail) != 1 || !strings.Contains(mail[0].Body, "second run passed") {
		t.Errorf("expected the mail to the Mayor to name both runs, got %+v", mail)
	}
}

func TestAGateThatFailsTwiceOnABusyHostRefuses(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false}, []application.LoadReading{calm(), busy(), calm()}, 15*time.Minute)

	if run.err == nil || run.report.Landed || !run.report.Refused || run.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", run.err, run.report)
	}
	if checks.runs != 2 {
		t.Fatalf("expected the gate run twice, got %d", checks.runs)
	}
	if !strings.Contains(run.report.Why, "the second run failed") {
		t.Errorf("expected the refusal to name both runs, got %q", run.report.Why)
	}
}

func TestAGateThatFailsOnACalmHostRefusesAtOnceAfterOneRun(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false, true}, []application.LoadReading{calm()}, 15*time.Minute)

	if run.err == nil || run.report.Landed || run.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", run.err, run.report)
	}
	if checks.runs != 1 {
		t.Fatalf("expected one run of the gate on a calm host, got %d", checks.runs)
	}
	if strings.Contains(run.report.Why, "twice") {
		t.Errorf("expected no second run to be named, got %q", run.report.Why)
	}
}

func TestTheWaitBeforeTheGateGivesUpAfterItsBoundAndRunsTheGateAnyway(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{true}, []application.LoadReading{busy()}, 2*time.Minute)

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", run.err, run.report)
	}
	if run.waits != 4 {
		t.Errorf("expected four waits of 30s to make the 2m bound, got %d", run.waits)
	}
	if checks.runs != 1 {
		t.Errorf("expected the gate run once, anyway, got %d", checks.runs)
	}
	for _, said := range []string{run.out.String(), run.vault.ledger[0]} {
		if !strings.Contains(said, "ran anyway") {
			t.Errorf("expected the give-up to be said, got %q", said)
		}
	}
}

func TestTheWaitBeforeTheGateEndsAsSoonAsTheHostCalms(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{true}, []application.LoadReading{busy(), busy(), calm()}, 15*time.Minute)

	if run.err != nil || !run.report.Landed || run.waits != 2 || checks.runs != 1 {
		t.Fatalf("expected two waits and one run, landed; got %d waits, %d runs, err %v", run.waits, checks.runs, run.err)
	}
}
