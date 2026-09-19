package rig_test

import (
	"context"
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
