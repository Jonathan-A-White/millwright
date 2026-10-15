package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestARigsTestsAreWhateverTheHostWasToldTheyAre(t *testing.T) {
	checks := rig.NewChecks(rig.WithCommands(map[string]string{"millwright": "echo green"}))

	if got := checks.Command("millwright"); got != "echo green" {
		t.Errorf("expected the rig's own command, got %q", got)
	}
	if got := checks.Command("fellowship"); got != rig.DefaultCommand {
		t.Errorf("expected a rig nobody configured to be tested with %q, got %q", rig.DefaultCommand, got)
	}

	checked, err := checks.Run(context.Background(), "millwright", t.TempDir())
	if err != nil {
		t.Fatalf("running the rig's tests: %v", err)
	}
	if !checked.Passed || !strings.Contains(checked.Output, "green") {
		t.Errorf("expected a passing run with its output, got %+v", checked)
	}
}

func TestTestsThatFailAreAnAnswerAndNotAFailure(t *testing.T) {
	checks := rig.NewChecks(rig.WithCommand("echo '--- FAIL: TestSomething'; exit 1"))

	checked, err := checks.Run(context.Background(), "millwright", t.TempDir())
	if err != nil {
		t.Fatalf("expected a failing test run to be reported, not to fail the run: %v", err)
	}
	switch {
	case checked.Passed:
		t.Error("expected the run not to have passed")
	case !strings.Contains(checked.Tail(5), "--- FAIL"):
		t.Errorf("expected the output to be kept, got %q", checked.Output)
	case checked.Command != "echo '--- FAIL: TestSomething'; exit 1":
		t.Errorf("expected the command to be reported so a person can run it, got %q", checked.Command)
	}
}

func TestTestsThatCannotBeRunAtAllAreAFailure(t *testing.T) {
	if _, err := rig.NewChecks().Run(context.Background(), "millwright", "/no/such/worktree"); err == nil {
		t.Error("expected a worktree that is not there to fail the run rather than count as red")
	}
	if _, err := rig.NewChecks().Run(context.Background(), "millwright", ""); err == nil {
		t.Error("expected running the tests nowhere to be refused")
	}
}

func TestACommandTheShellCouldNotFindIsNotARedTest(t *testing.T) {
	for _, code := range []string{"126", "127"} {
		checks := rig.NewChecks(rig.WithCommand("echo 'make: go: No such file or directory'; exit " + code))

		checked, err := checks.Run(context.Background(), "millwright", t.TempDir())
		if err != nil {
			t.Fatalf("exit %s: expected the run to be reported, not to fail: %v", code, err)
		}
		switch {
		case checked.Passed:
			t.Errorf("exit %s: expected the run not to have passed", code)
		case !checked.NotRun:
			t.Errorf("exit %s: expected the run to be reported as one that could not be made, got %+v", code, checked)
		case !strings.Contains(checked.Tail(5), "No such file or directory"):
			t.Errorf("exit %s: expected the command's own output to be kept, got %q", code, checked.Output)
		}
	}
}

func TestATestThatExitsWithAnyOtherCodeHasRun(t *testing.T) {
	checked, err := rig.NewChecks(rig.WithCommand("exit 2")).Run(context.Background(), "millwright", t.TempDir())
	if err != nil {
		t.Fatalf("running the rig's tests: %v", err)
	}
	if checked.Passed || checked.NotRun {
		t.Errorf("expected a red run that did run, got %+v", checked)
	}
}

// A rig's own tests are not a sync: the low-speed limit that ends a stalled
// vault fetch belongs to the vault's git runner alone, and giving it to a
// rig's build or test command as well would fail a slow but healthy build
// that happens to write output slowly to git (a large checkout, say).
func TestARigsTestsDoNotCarryTheVaultsLowSpeedLimit(t *testing.T) {
	checks := rig.NewChecks(rig.WithCommand(`echo "limit=$GIT_HTTP_LOW_SPEED_LIMIT time=$GIT_HTTP_LOW_SPEED_TIME"`))

	checked, err := checks.Run(context.Background(), "millwright", t.TempDir())
	if err != nil {
		t.Fatalf("running the rig's tests: %v", err)
	}
	if want := "limit= time=\n"; checked.Output != want {
		t.Fatalf("expected a rig's own command to see no low-speed limit, got %q", checked.Output)
	}
}

// TestTheGateRunsUnderTheNiceItIsGiven: mw next's gate is a Builder's tests in
// all but name, and runs behind the mill's work just as the session did
// (mw-gq6.312). The command reads its own niceness back.
func TestTheGateRunsUnderTheNiceItIsGiven(t *testing.T) {
	base := niceOf(t, rig.NewChecks(rig.WithCommand("nice")))
	niced := niceOf(t, rig.NewChecks(rig.WithCommand("nice"), rig.WithNice(7)))
	if niced != base+7 && niced != 19 {
		t.Errorf("expected the gate to run %d nicer than %d, got %d", 7, base, niced)
	}
	if off := niceOf(t, rig.NewChecks(rig.WithCommand("nice"), rig.WithNice(0))); off != base {
		t.Errorf("expected a nice of 0 to leave the gate at %d, got %d", base, off)
	}
}

func niceOf(t *testing.T, checks *rig.Checks) int {
	t.Helper()
	checked, err := checks.Run(context.Background(), "millwright", t.TempDir())
	if err != nil || !checked.Passed {
		t.Fatalf("running nice: %v %+v", err, checked)
	}
	n, err := strconv.Atoi(strings.TrimSpace(checked.Output))
	if err != nil {
		t.Fatalf("expected the command to print its niceness, got %q", checked.Output)
	}
	return n
}

// TestTheGateRunsWithTheWorkerLimitItIsGiven: the gate's tests start no more
// workers than the host's builder_jobs allows (mw-gq6.399); with none given the
// command sees the environment it was started in.
func TestTheGateRunsWithTheWorkerLimitItIsGiven(t *testing.T) {
	t.Setenv("JOBS", "")
	t.Setenv("GOMAXPROCS", "")
	t.Setenv("VITEST_MAX_WORKERS", "")
	command := `echo "$JOBS $GOMAXPROCS $VITEST_MAX_WORKERS"`
	for jobs, want := range map[int]string{3: "3 3 3", 0: "  "} {
		checked, err := rig.NewChecks(rig.WithCommand(command), rig.WithJobs(jobs)).Run(context.Background(), "millwright", t.TempDir())
		if err != nil || !checked.Passed {
			t.Fatalf("running the command: %v %+v", err, checked)
		}
		if got := strings.TrimSuffix(checked.Output, "\n"); got != want {
			t.Errorf("with %d jobs the command saw %q, want %q", jobs, got, want)
		}
	}
}

// pidFrom waits for the command to write the pid of the child it started in a
// process group of its own, and reads it.
func pidFrom(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the command never wrote %s", path)
	return 0
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// TestACancelledCheckLetsItsCommandStopWhatItStarted: a check that is cancelled
// is told to stop (SIGTERM to its group) before it is killed, so a command like
// Playwright can stop the webServer it started in a process group of its own
// (mw-gq6.410). The command here is that Playwright: a child in its own group,
// which the command stops on TERM. Killed outright, the child would be left running.
func TestACancelledCheckLetsItsCommandStopWhatItStarted(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "server.pid")
	command := `setsid sh -c 'echo $$ > "$1"; exec sleep 300' sh "` + pidfile + `" &
child=$!
trap 'kill $child; wait $child; exit 0' TERM
while :; do sleep 0.1; done`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := rig.NewChecks(rig.WithCommand(command)).Run(ctx, "millwright", t.TempDir())
		done <- err
	}()

	pid := pidFrom(t, pidfile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the cancelled check never returned")
	}
	if alive(pid) {
		t.Errorf("expected the child in its own process group to have been stopped by the time the check returned, pid %d is still running", pid)
	}
}

// TestACancelledCheckThatIgnoresTermIsKilledAfterTheGrace: the TERM is a request;
// a group that ignores it is killed once the grace is up, and the check returns.
func TestACancelledCheckThatIgnoresTermIsKilledAfterTheGrace(t *testing.T) {
	rig.SetTermGraceForTest(t, time.Second)
	pidfile := filepath.Join(t.TempDir(), "stubborn.pid")
	command := `trap '' TERM
echo $$ > "` + pidfile + `"
while :; do sleep 0.1; done`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		_, err := rig.NewChecks(rig.WithCommand(command)).Run(ctx, "millwright", t.TempDir())
		done <- result{err, time.Since(start)}
	}()

	pid := pidFrom(t, pidfile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancelled := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the cancelled check never returned")
	}
	// The ceiling is thirty times the grace: it only has to tell a kill that came
	// from one that never did, and a busy host must not cross it (mw-gq6.423).
	if took := time.Since(cancelled); took < time.Second || took > 30*time.Second {
		t.Errorf("expected the check to return after the 1s grace and within 30s of it, took %s", took)
	}
	if alive(pid) {
		t.Errorf("expected the group that ignored TERM to be killed, pid %d is still running", pid)
	}
}
