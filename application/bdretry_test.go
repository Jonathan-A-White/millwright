package application_test

import (
	"context"
	"strings"
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
