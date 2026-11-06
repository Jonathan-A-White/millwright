package application_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.393: a Builder session's bd writes met a passing fault of the beads
// server on the desktop and were lost, so a finished story stopped for open
// steps. mw bd is the bd the session runs: it tries a write again for
// two minutes before it gives up, and never writes a comment twice.

type bdRetryRun struct {
	bd     *apptest.FakeBd
	waited time.Duration
	out    strings.Builder
	errs   strings.Builder
	exit   int
	err    error
}

func runBdRetry(t *testing.T, bd *apptest.FakeBd, args ...string) *bdRetryRun {
	t.Helper()
	run := &bdRetryRun{bd: bd}
	run.exit, run.err = application.BdRetry{
		Program: bd,
		Wait: func(_ context.Context, d time.Duration) error {
			run.waited += d
			return nil
		},
		Out: &run.out, Err: &run.errs,
	}.Run(context.Background(), args)
	return run
}

func TestAStepCloseThatMeetsAPassingFaultIsTriedAgain(t *testing.T) {
	bd := apptest.NewFakeBd("step-1")
	bd.FailWrites(
		apptest.BdFault{Said: "dolt server unreachable"},
		apptest.BdFault{Said: "dial tcp 10.88.0.2:3307: connect: connection refused"},
	)
	run := runBdRetry(t, bd, "-C", "/vault", "close", "step-1", "--reason", "done")
	if run.err != nil || run.exit != 0 {
		t.Fatalf("exit %d, err %v, said %q", run.exit, run.err, run.errs.String())
	}
	if got := bd.Status("step-1"); got != "closed" {
		t.Errorf("step-1 is %s, want closed", got)
	}
	if !strings.Contains(run.errs.String(), "connection refused") {
		t.Errorf("the retry should say what it met, it said %q", run.errs.String())
	}
}

func TestAStepCloseThatLandedButReportedIndeterminateIsNotTriedAgain(t *testing.T) {
	bd := apptest.NewFakeBd("step-1")
	bd.FailWrites(apptest.BdFault{Said: "write commit result indeterminate", Landed: true})
	run := runBdRetry(t, bd, "close", "step-1")
	if run.err != nil || run.exit != 0 {
		t.Fatalf("exit %d, err %v, said %q", run.exit, run.err, run.errs.String())
	}
	for _, call := range bd.Calls() {
		if call[0] == "close" && len(bd.Calls()) > 2 {
			t.Errorf("a close that had landed was run again: %v", bd.Calls())
		}
	}
}

func TestABdThatStaysDownFailsLoudlyNamingTheFault(t *testing.T) {
	bd := apptest.NewFakeBd("step-1")
	bd.GoDown("dolt server unreachable")
	run := runBdRetry(t, bd, "close", "step-1")
	if run.exit == 0 {
		t.Fatalf("a close that never landed exited 0")
	}
	if !strings.Contains(run.errs.String(), "dolt server unreachable") || !strings.Contains(run.errs.String(), "giving up") {
		t.Errorf("it should name the fault and say it gave up, it said %q", run.errs.String())
	}
	if run.waited < application.BdRetryLimit-time.Minute || run.waited > application.BdRetryLimit+time.Minute {
		t.Errorf("it waited %s, want about %s", run.waited, application.BdRetryLimit)
	}
	if got := bd.Status("step-1"); got != "open" {
		t.Errorf("step-1 is %s, want open", got)
	}
}

func TestABdThatSaysNoSuchBeadIsNotTriedAgain(t *testing.T) {
	bd := apptest.NewFakeBd()
	bd.FailWrites(apptest.BdFault{Said: "no issue found matching \"nope\""})
	run := runBdRetry(t, bd, "close", "nope")
	if run.exit == 0 || run.waited != 0 || len(bd.Calls()) != 1 {
		t.Errorf("exit %d, waited %s, %d calls: a refusal is not a passing fault", run.exit, run.waited, len(bd.Calls()))
	}
}

func TestACommentWhoseFirstWriteLandedIsNeverWrittenTwice(t *testing.T) {
	for _, args := range [][]string{
		{"-C", "/vault", "comment", "story-1", "All done"},
		{"comments", "add", "story-1", "All done"},
	} {
		bd := apptest.NewFakeBd("story-1")
		bd.FailWrites(apptest.BdFault{Said: "write commit result indeterminate", Landed: true})
		run := runBdRetry(t, bd, args...)
		if run.err != nil || run.exit != 0 {
			t.Fatalf("%v: exit %d, err %v, said %q", args, run.exit, run.err, run.errs.String())
		}
		if got := bd.Comments("story-1"); len(got) != 1 {
			t.Errorf("%v: the story holds %d comments %q, want exactly one", args, len(got), got)
		}
	}
}

func TestACommentWhoseFirstWriteDidNotLandIsWrittenOnce(t *testing.T) {
	bd := apptest.NewFakeBd("story-1")
	bd.FailWrites(apptest.BdFault{Said: "dolt server unreachable"}, apptest.BdFault{Said: "i/o timeout"})
	run := runBdRetry(t, bd, "comment", "story-1", "All done")
	if run.err != nil || run.exit != 0 {
		t.Fatalf("exit %d, err %v, said %q", run.exit, run.err, run.errs.String())
	}
	if got := bd.Comments("story-1"); len(got) != 1 || got[0] != "All done" {
		t.Errorf("the story holds %q, want one \"All done\"", got)
	}
}

func TestACreateIsNeverTriedAgain(t *testing.T) {
	bd := apptest.NewFakeBd()
	bd.FailWrites(apptest.BdFault{Said: "write commit result indeterminate", Landed: true})
	run := runBdRetry(t, bd, "create", "A bead")
	if run.exit == 0 || len(bd.Calls()) != 1 {
		t.Errorf("exit %d after %d calls: a create that may have landed must not be run again", run.exit, len(bd.Calls()))
	}
}

func TestAReadThatMeetsAPassingFaultIsTriedAgain(t *testing.T) {
	bd := apptest.NewFakeBd("step-1")
	bd.FailReads(apptest.BdFault{Said: "dolt server unreachable"})
	run := runBdRetry(t, bd, "show", "step-1", "--json")
	if run.err != nil || run.exit != 0 || !strings.Contains(run.out.String(), "step-1") {
		t.Errorf("exit %d, err %v, out %q", run.exit, run.err, run.out.String())
	}
}

func TestACloseWhoseCheckAlsoMeetsTheFaultWaitsAndChecksAgain(t *testing.T) {
	bd := apptest.NewFakeBd("step-1")
	bd.FailWrites(apptest.BdFault{Said: "write commit result indeterminate", Landed: true})
	bd.FailReads(apptest.BdFault{Said: "dolt server unreachable"})
	run := runBdRetry(t, bd, "close", "step-1")
	if run.exit != 0 {
		t.Fatalf("exit %d, said %q", run.exit, run.errs.String())
	}
	for _, call := range bd.Calls() {
		if len(call) > 0 && call[0] == "close" && len(bd.Calls()) > 3 {
			t.Errorf("a close that had landed was run again: %v", bd.Calls())
		}
	}
}

func TestACommentThatCannotBeReadBackIsNotWrittenBlind(t *testing.T) {
	bd := apptest.NewFakeBd("story-1")
	bd.FailWrites(apptest.BdFault{Said: "write commit result indeterminate", Landed: true})
	run := runBdRetry(t, bd, "comment", "story-1", "-f", "notes.txt")
	if run.exit == 0 || len(bd.Comments("story-1")) != 1 || len(bd.Calls()) != 1 {
		t.Errorf("exit %d, %d comments, %d calls: a comment from a file cannot be compared, so it is not run again",
			run.exit, len(bd.Comments("story-1")), len(bd.Calls()))
	}
	if !strings.Contains(run.errs.String(), "indeterminate") {
		t.Errorf("it should name the fault, it said %q", run.errs.String())
	}
}

func TestAStepCloseThatMeetsAnOpenCircuitBreakerIsTriedAgain(t *testing.T) {
	bd := apptest.NewFakeBd("step-1")
	bd.FailWrites(apptest.BdFault{Said: "dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)"})
	run := runBdRetry(t, bd, "-C", "/vault", "close", "step-1", "--reason", "done")
	if run.err != nil || run.exit != 0 {
		t.Fatalf("exit %d, err %v, said %q", run.exit, run.err, run.errs.String())
	}
	if got := bd.Status("step-1"); got != "closed" {
		t.Errorf("step-1 is %s, want closed", got)
	}
}

// mw-gq6.568: Clerks and seats ran a bd beside another bd though their prompts
// told them not to. mw bd now waits for its turn and for quiet (no other bd, no
// mw sync) before it runs bd, up to a limit, and fails naming what blocked it.

type bdQuietRun struct {
	*bdRetryRun
	procs *apptest.FakeUserProcesses
	turn  *apptest.FakeBdTurn
}

// runBdQuiet runs bd args with the quiet gate on. leave is how many pauses pass
// before the processes of the table end, 0 for never.
func runBdQuiet(t *testing.T, procs *apptest.FakeUserProcesses, turn *apptest.FakeBdTurn, limit time.Duration, leave int, args ...string) *bdQuietRun {
	t.Helper()
	bd := apptest.NewFakeBd("step-1")
	run := &bdQuietRun{bdRetryRun: &bdRetryRun{bd: bd}, procs: procs, turn: turn}
	pauses := 0
	run.exit, run.err = application.BdRetry{
		Program: bd,
		Quiet:   procs,
		Turn:    turn,
		Wait: func(_ context.Context, d time.Duration) error {
			run.waited += d
			pauses++
			if leave > 0 && pauses == leave {
				for _, pid := range procs.Alive() {
					procs.Stop(context.Background(), pid)
				}
			}
			return nil
		},
		QuietWait: limit,
		Out:       &run.out, Err: &run.errs,
	}.Run(context.Background(), args)
	return run
}

func TestAnotherBdRunningMakesBdWaitUntilItEnds(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 41, Argv: []string{"/usr/local/bin/bd", "list"}, Cwd: "/home/jwhite/millwright-vault"})
	run := runBdQuiet(t, procs, apptest.NewFakeBdTurn(), 0, 3, "close", "step-1")
	if run.err != nil || run.exit != 0 {
		t.Fatalf("exit %d, err %v, said %q", run.exit, run.err, run.errs.String())
	}
	if run.waited == 0 {
		t.Errorf("bd ran at once beside another bd")
	}
	if got := bd1Status(run); got != "closed" {
		t.Errorf("step-1 is %s, want closed after the wait", got)
	}
	if !strings.Contains(run.errs.String(), "waited") {
		t.Errorf("a wait should be said on standard error, it said %q", run.errs.String())
	}
}

func bd1Status(run *bdQuietRun) string { return run.bd.Status("step-1") }

func TestAnMwSyncRunningMakesBdWait(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 42, Argv: []string{"/home/jwhite/go/bin/mw", "sync"}})
	run := runBdQuiet(t, procs, apptest.NewFakeBdTurn(), 0, 2, "show", "step-1")
	if run.err != nil || run.exit != 0 || run.waited == 0 {
		t.Fatalf("exit %d, err %v, waited %s: bd must wait for a sync", run.exit, run.err, run.waited)
	}
}

func TestProcessesThatAreNotAnotherBdDoNotMakeBdWait(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	for _, p := range []application.Process{
		{Pid: 50, Argv: []string{"/usr/bin/mw", "status"}},
		{Pid: 51, Argv: []string{"/usr/bin/mw", "bd", "list"}},
		{Pid: 52, Argv: []string{"/usr/bin/bdx", "list"}},
		{Pid: 53, Argv: []string{"/usr/bin/git", "bd"}},
		// A bd of a test suite's throwaway vault is not the factory's.
		{Pid: 54, Argv: []string{"/usr/local/bin/bd", "list"}, Cwd: "/tmp/TestSomething123/001"},
		{Pid: 55, Argv: nil},
	} {
		procs.Start(p)
	}
	run := runBdQuiet(t, procs, apptest.NewFakeBdTurn(), 0, 0, "show", "step-1")
	if run.err != nil || run.exit != 0 || run.waited != 0 {
		t.Fatalf("exit %d, err %v, waited %s: nothing there is a bd or a sync", run.exit, run.err, run.waited)
	}
}

func TestBdThatCannotGetQuietFailsNamingTheBlocker(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 77, Argv: []string{"/usr/local/bin/bd", "update", "mw-1"}})
	run := runBdQuiet(t, procs, apptest.NewFakeBdTurn(), 0, 0, "close", "step-1")
	if run.err == nil || run.exit == 0 {
		t.Fatalf("exit %d, err %v: a bd that never got quiet must fail", run.exit, run.err)
	}
	for _, want := range []string{"77", "bd update mw-1"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("the error should name the blocker (%q), it said %q", want, run.err)
		}
	}
	if run.waited < application.BdQuietLimit-time.Second || run.waited > application.BdQuietLimit+time.Second {
		t.Errorf("it waited %s, want the default %s", run.waited, application.BdQuietLimit)
	}
	if len(run.bd.Calls()) != 0 {
		t.Errorf("bd was run beside the other: %v", run.bd.Calls())
	}
	if got := bd1Status(run); got != "open" {
		t.Errorf("step-1 is %s, want open", got)
	}
}

func TestTheQuietLimitCanBeChangedAndSwitchedOff(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 77, Argv: []string{"/usr/local/bin/bd", "list"}})
	run := runBdQuiet(t, procs, apptest.NewFakeBdTurn(), 10*time.Second, 0, "show", "step-1")
	if run.err == nil || run.waited < 9*time.Second || run.waited > 11*time.Second {
		t.Errorf("err %v, waited %s: want a failure after about 10s", run.err, run.waited)
	}
	run = runBdQuiet(t, procs, apptest.NewFakeBdTurn(), -1, 0, "show", "step-1")
	if run.err != nil || run.exit != 0 || run.waited != 0 {
		t.Errorf("err %v, exit %d, waited %s: a negative limit means no waiting at all", run.err, run.exit, run.waited)
	}
}

func TestBdWaitsForTheTurnAnotherMwBdHolds(t *testing.T) {
	turn := apptest.NewFakeBdTurn()
	release := turn.Hold()
	defer release()
	run := runBdQuiet(t, apptest.NewFakeUserProcesses(), turn, 5*time.Second, 0, "show", "step-1")
	if run.err == nil || run.exit == 0 {
		t.Fatalf("exit %d, err %v: the turn is held, bd must not run", run.exit, run.err)
	}
	if !strings.Contains(run.err.Error(), "another mw bd") || len(run.bd.Calls()) != 0 {
		t.Errorf("err %q, calls %v: it should name the holder and not run", run.err, run.bd.Calls())
	}
}

func TestTheTurnIsLetGoWhenBdIsDone(t *testing.T) {
	turn := apptest.NewFakeBdTurn()
	runBdQuiet(t, apptest.NewFakeUserProcesses(), turn, 0, 0, "show", "step-1")
	release, taken, _, _ := turn.TryTake(context.Background())
	if !taken {
		t.Fatalf("mw bd kept the turn after it was done")
	}
	release()
}

// slowBd is a bd program that takes a while and notes how many were running at
// once.
type slowBd struct {
	mu          sync.Mutex
	running     int
	most, calls int
}

func (s *slowBd) Run(context.Context, []string) (application.BdRun, error) {
	s.mu.Lock()
	s.running++
	s.calls++
	s.most = max(s.most, s.running)
	s.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	s.mu.Lock()
	s.running--
	s.mu.Unlock()
	return application.BdRun{Stdout: "ok\n"}, nil
}

func TestTwoMwBdCallsAtOnceRunOneAfterTheOther(t *testing.T) {
	turn := apptest.NewFakeBdTurn()
	program := &slowBd{}
	var wg sync.WaitGroup
	exits := make([]int, 4)
	for i := range exits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exits[i], _ = application.BdRetry{
				Program: program,
				Quiet:   apptest.NewFakeUserProcesses(),
				Turn:    turn,
				Poll:    time.Millisecond,
				Out:     io.Discard, Err: io.Discard,
			}.Run(context.Background(), []string{"update", "mw-1", "--status", "open"})
		}()
	}
	wg.Wait()
	if program.most != 1 {
		t.Errorf("%d bd ran together, want one at a time", program.most)
	}
	if program.calls != len(exits) {
		t.Errorf("bd ran %d times, want %d: every call gets its turn", program.calls, len(exits))
	}
	for i, exit := range exits {
		if exit != 0 {
			t.Errorf("call %d left with %d", i, exit)
		}
	}
}
