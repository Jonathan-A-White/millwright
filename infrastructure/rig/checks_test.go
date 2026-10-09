package rig_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

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
