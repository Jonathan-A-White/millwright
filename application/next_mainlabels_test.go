package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.929: a stop on failing tests says, for each failing test, whether it
// fails on main too, so that the Mayor need not send a Clerk to find out. The
// tests are those of a close-out whose worktree run fails two tests.

// aMainLanding is a landing remote that counts the throwaway worktrees it
// opened and took away again.
type aMainLanding struct {
	fakeRetryLanding
	mu2            sync.Mutex
	opened, closed int
}

func (l *aMainLanding) OpenLanding(context.Context, string, string) (string, error) {
	l.mu2.Lock()
	defer l.mu2.Unlock()
	l.opened++
	return "/landing", nil
}

func (l *aMainLanding) CloseLanding(context.Context, string, string) error {
	l.mu2.Lock()
	defer l.mu2.Unlock()
	l.closed++
	return nil
}

// twoFailingTests are the rig's tests failing TestA and TestB in the worktree.
type twoFailingTests struct{ afterwards func() }

func (c twoFailingTests) Run(context.Context, string, string) (application.Checked, error) {
	if c.afterwards != nil {
		defer c.afterwards()
	}
	return application.Checked{Command: "make test", Output: "--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n"}, nil
}

// namedTestsThat answers a run of the tests by name as a script says; it
// remembers how many times and with which names it was asked.
type namedTestsThat struct {
	run   func(ctx context.Context, tests []string) (application.NamedRun, error)
	asked [][]string
}

func (n *namedTestsThat) RunNamed(ctx context.Context, _, _ string, tests []string) (application.NamedRun, error) {
	n.asked = append(n.asked, tests)
	return n.run(ctx, tests)
}

func failingOnMain(output string) func(context.Context, []string) (application.NamedRun, error) {
	return func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{Checked: application.Checked{Command: "go test -run", Output: output}, Ran: true}, nil
	}
}

type closeOutOnMain struct {
	report  application.NextReport
	err     error
	landing *aMainLanding
	mail    string
}

func aTestsFailStop(t *testing.T, named application.NamedChecks, load application.HostLoad, bound time.Duration, afterwards func()) closeOutOnMain {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	mailbox := apptest.NewFakeMailbox()
	got := closeOutOnMain{landing: landing}
	got.report, got.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: twoFailingTests{afterwards: afterwards}, Slot: aSlot{}, Mailbox: mailbox,
		NamedChecks: named, Load: load, MainAloneBound: bound,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Seat:     "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range inbox {
		got.mail += m.Body + "\n"
	}
	return got
}

func TestAStopOnFailingTestsLabelsEachAsFailingOnMainOrOnTheBranchOnly(t *testing.T) {
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\nFAIL\n")}
	got := aTestsFailStop(t, named, nil, 0, nil)

	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected the story to stop for tests-fail, as nothing is excused, got %q (%v)", got.report.Reason, got.err)
	}
	if len(named.asked) != 1 || strings.Join(named.asked[0], ",") != "TestA,TestB" {
		t.Fatalf("expected one run of just TestA and TestB on main, got %q", named.asked)
	}
	for _, want := range []string{"TestA: fails on main too", "TestB: branch only"} {
		if !strings.Contains(got.mail, want) {
			t.Errorf("expected the refused mail to say %q, got:\n%s", want, got.mail)
		}
	}
	if got.landing.opened != 1 || got.landing.closed != 1 {
		t.Errorf("expected the temporary worktree opened and taken away once, got %d opened and %d closed", got.landing.opened, got.landing.closed)
	}
}

func TestAStopLabelsTestsAsBranchOnlyWhenMainPassesThem(t *testing.T) {
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{Checked: application.Checked{Command: "go test -run", Passed: true}, Ran: true}, nil
	}}
	got := aTestsFailStop(t, named, nil, 0, nil)
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected tests-fail, got %q (%v)", got.report.Reason, got.err)
	}
	for _, want := range []string{"TestA: branch only", "TestB: branch only"} {
		if !strings.Contains(got.mail, want) {
			t.Errorf("expected the refused mail to say %q, got:\n%s", want, got.mail)
		}
	}
}

func TestAStopLabelsTestsAsNotRunWhenTheHostHasNoRoom(t *testing.T) {
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA\n")}
	load := &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 12, Cores: 8}}
	got := aTestsFailStop(t, named, load, 0, nil)
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected tests-fail, got %q (%v)", got.report.Reason, got.err)
	}
	if len(named.asked) != 0 {
		t.Fatalf("expected no run on a host with no room, got %q", named.asked)
	}
	for _, want := range []string{"TestA: not run: ", "TestB: not run: ", "load 12.0 of 8 cores"} {
		if !strings.Contains(got.mail, want) {
			t.Errorf("expected the refused mail to say %q, got:\n%s", want, got.mail)
		}
	}
	if got.landing.opened != 0 {
		t.Errorf("expected no worktree opened for a run that is not made, got %d", got.landing.opened)
	}
}

func TestAStopLabelsTestsAsNotRunWhenTheRunFailsAndStopsAsBefore(t *testing.T) {
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{}, errors.New("sh: go: not found")
	}}
	got := aTestsFailStop(t, named, nil, 0, nil)
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a failed check to change nothing: tests-fail, got %q (%v)", got.report.Reason, got.err)
	}
	for _, want := range []string{"TestA: not run: ", "sh: go: not found"} {
		if !strings.Contains(got.mail, want) {
			t.Errorf("expected the refused mail to say %q, got:\n%s", want, got.mail)
		}
	}
	if got.landing.closed != got.landing.opened {
		t.Errorf("expected every worktree opened taken away, got %d opened and %d closed", got.landing.opened, got.landing.closed)
	}
}

func TestAStopLabelsTestsAsNotRunWhenTheRunOutlastsItsBound(t *testing.T) {
	named := &namedTestsThat{run: func(ctx context.Context, _ []string) (application.NamedRun, error) {
		<-ctx.Done()
		return application.NamedRun{}, ctx.Err()
	}}
	got := aTestsFailStop(t, named, nil, 20*time.Millisecond, nil)
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected tests-fail, got %q (%v)", got.report.Reason, got.err)
	}
	if !strings.Contains(got.mail, "TestA: not run: ") || !strings.Contains(got.mail, "did not end in") {
		t.Errorf("expected the refused mail to say the run was cut at its bound, got:\n%s", got.mail)
	}
	if got.landing.closed != got.landing.opened || got.landing.opened != 1 {
		t.Errorf("expected the worktree opened and taken away once, got %d opened and %d closed", got.landing.opened, got.landing.closed)
	}
}

func TestAStopHeldOnMainsRedStillSaysEachTestFailsOnMain(t *testing.T) {
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n")}
	got := aTestsFailStop(t, named, nil, 0, nil)
	if got.report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red as before, got %q (%v)", got.report.Reason, got.err)
	}
	if !strings.Contains(got.mail, "TestA: fails on main too") || !strings.Contains(got.mail, "TestB: fails on main too") {
		t.Errorf("expected both tests labelled in the mail, got:\n%s", got.mail)
	}
}
