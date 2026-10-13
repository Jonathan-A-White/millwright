package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aGateLock is the host's gate lock as a close-out sees it: it counts what was
// taken and given back, and can say another gate held it, or refuse to be taken.
type aGateLock struct {
	heldBy  string
	waited  time.Duration
	refuse  error
	taken   int
	freed   int
	holders []string
}

func (g *aGateLock) Take(_ context.Context, holder string, held func(string)) (application.GateTurn, error) {
	if g.refuse != nil {
		return application.GateTurn{}, g.refuse
	}
	g.taken++
	g.holders = append(g.holders, holder)
	if g.heldBy != "" && held != nil {
		held(g.heldBy)
	}
	return application.GateTurn{Holding: &aGateHolding{g}, Waited: g.waited, For: g.heldBy}, nil
}

type aGateHolding struct{ g *aGateLock }

func (h *aGateHolding) Release(context.Context) error { h.g.freed++; return nil }
func (h *aGateHolding) HeldBy() string                { return "" }

// aChecksThatCannotRun is a rig whose tests cannot be started.
type aChecksThatCannotRun struct{}

func (aChecksThatCannotRun) Run(context.Context, string, string) (application.Checked, error) {
	return application.Checked{}, errors.New("no such command")
}

func aCloseOutUnderGateLock(t *testing.T, lock *aGateLock, checks application.Checks) (*gateRun, *strings.Builder) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	run := &gateRun{vault: vault, out: &strings.Builder{}, mailbox: apptest.NewFakeMailbox()}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checks, Slot: aSlot{}, Mailbox: run.mailbox, GateLock: lock,
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  run.out, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return run, run.out
}

func TestTheGateLockIsGivenBackWhenTheGatePasses(t *testing.T) {
	lock := &aGateLock{}
	run, _ := aCloseOutUnderGateLock(t, lock, &scriptedChecks{passes: []bool{true}})
	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected a landing, got %v and %+v", run.err, run.report)
	}
	if lock.taken != 1 || lock.freed != 1 || lock.holders[0] != "mw-x.1" {
		t.Errorf("expected the lock taken once for mw-x.1 and given back, got %+v", lock)
	}
}

func TestTheGateLockIsGivenBackWhenTheGateFails(t *testing.T) {
	lock := &aGateLock{}
	run, _ := aCloseOutUnderGateLock(t, lock, &scriptedChecks{passes: []bool{false}})
	if run.err == nil || run.report.Landed {
		t.Fatalf("expected a refusal, got %v and %+v", run.err, run.report)
	}
	if lock.taken != 1 || lock.freed != 1 {
		t.Errorf("expected the lock taken and given back, got %+v", lock)
	}
}

func TestTheGateLockIsGivenBackWhenTheGateCannotBeRun(t *testing.T) {
	lock := &aGateLock{}
	run, _ := aCloseOutUnderGateLock(t, lock, aChecksThatCannotRun{})
	if run.err == nil || run.report.Landed || run.report.Reason != application.ReasonTestsNotRun {
		t.Fatalf("expected a tests-not-run stop, got %v and %+v", run.err, run.report)
	}
	if lock.taken != 1 || lock.freed != 1 {
		t.Errorf("expected the lock taken and given back on the error path, got %+v", lock)
	}
}

func TestAGateThatWaitedSaysForWhomAndHowLong(t *testing.T) {
	lock := &aGateLock{heldBy: "mw-y.2", waited: 7 * time.Minute}
	run, out := aCloseOutUnderGateLock(t, lock, &scriptedChecks{passes: []bool{true}})
	if !run.report.Landed {
		t.Fatalf("expected a landing, got %v and %+v", run.err, run.report)
	}
	for _, text := range []string{run.report.String(), out.String()} {
		if !strings.Contains(text, "gate waited 7m for mw-y.2") {
			t.Errorf("expected the report to say the gate waited 7m for mw-y.2, got:\n%s", text)
		}
	}
	if mail, _ := run.mailbox.Inbox(context.Background(), application.MayorMailbox); len(mail) != 1 || !strings.Contains(mail[0].Body, "gate waited 7m for mw-y.2") {
		t.Errorf("expected the mail to the Mayor to say so, got %+v", mail)
	}
}

func TestAGateWhoseLockWasFreeSaysNothingOfWaiting(t *testing.T) {
	run, out := aCloseOutUnderGateLock(t, &aGateLock{}, &scriptedChecks{passes: []bool{true}})
	if strings.Contains(run.report.String(), "gate waited") || strings.Contains(out.String(), "waited") {
		t.Errorf("expected no word of waiting, got:\n%s", out.String())
	}
}

func TestAGateLockThatCannotBeTakenIsSaidAndTheGateRunsAnyway(t *testing.T) {
	lock := &aGateLock{refuse: errors.New("still held after 3h")}
	run, _ := aCloseOutUnderGateLock(t, lock, &scriptedChecks{passes: []bool{true}})
	if !run.report.Landed {
		t.Fatalf("expected the story landed anyway, got %v and %+v", run.err, run.report)
	}
	if !strings.Contains(run.report.String(), "gate lock could not be taken (still held after 3h)") {
		t.Errorf("expected the report to say so, got:\n%s", run.report.String())
	}
}

func TestAWaitIsSaidInMinutesAndSeconds(t *testing.T) {
	for wait, want := range map[time.Duration]string{
		300 * time.Millisecond:         "under a second",
		40 * time.Second:               "40s",
		7 * time.Minute:                "7m",
		7*time.Minute + 30*time.Second: "7m30s",
		time.Hour + 5*time.Minute:      "1h5m",
		2*time.Hour + 3*time.Second:    "2h3s",
	} {
		run, _ := aCloseOutUnderGateLock(t, &aGateLock{heldBy: "mw-y.2", waited: wait}, &scriptedChecks{passes: []bool{true}})
		if !strings.Contains(run.report.String(), "gate waited "+want+" for mw-y.2") {
			t.Errorf("a wait of %s: expected %q in\n%s", wait, want, run.report.String())
		}
	}
}
