package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestAHostNamesTheAfterLandingCommandOfEachRig(t *testing.T) {
	after := rig.NewAfterLanding(rig.WithAfterCommands(map[string]string{"millwright": "make build", "blank": "  "}))

	if got := after.Command("millwright"); got != "make build" {
		t.Errorf("expected the rig's own command, got %q", got)
	}
	for _, unnamed := range []string{"fellowship", "blank"} {
		if got := after.Command(unnamed); got != "" {
			t.Errorf("expected no command for %s, so that nothing is run, got %q", unnamed, got)
		}
	}
}

func TestTheAfterLandingCommandRunsInTheRigsDirectory(t *testing.T) {
	dir := t.TempDir()
	after := rig.NewAfterLanding(rig.WithAfterCommands(map[string]string{"millwright": "pwd; echo built"}))

	ran, err := after.Run(context.Background(), "millwright", dir)
	if err != nil {
		t.Fatalf("running the command: %v", err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if !ran.Succeeded() || !strings.Contains(ran.Output, want) || !strings.Contains(ran.Output, "built") {
		t.Errorf("expected a successful run in %s with its output, got %+v", want, ran)
	}
	if ran.Command != "pwd; echo built" {
		t.Errorf("expected the command to be reported, got %q", ran.Command)
	}
}

func TestAnAfterLandingCommandThatFailsIsAnAnswerAndNotAnError(t *testing.T) {
	after := rig.NewAfterLanding(rig.WithAfterCommands(map[string]string{"millwright": "echo 'no rule to make target'; exit 2"}))

	ran, err := after.Run(context.Background(), "millwright", t.TempDir())
	if err != nil {
		t.Fatalf("expected a failing command to be reported, not to fail the run: %v", err)
	}
	if ran.Succeeded() || ran.Status != 2 || ran.TimedOut != 0 || !strings.Contains(ran.Output, "no rule to make target") {
		t.Errorf("expected exit status 2 with the output kept, got %+v", ran)
	}
}

func TestAProgramThatIsNotThereIsAFailedRun(t *testing.T) {
	after := rig.NewAfterLanding(rig.WithAfterCommands(map[string]string{"millwright": "no-such-program-mw-test"}))

	ran, err := after.Run(context.Background(), "millwright", t.TempDir())
	if err != nil {
		t.Fatalf("expected a missing program to be reported, not to fail the run: %v", err)
	}
	if ran.Succeeded() || ran.Status != 127 {
		t.Errorf("expected the shell's exit status 127, got %+v", ran)
	}
	if line := ran.Line(); !strings.Contains(line, "exit status 127") || !strings.Contains(line, "no such program") {
		t.Errorf("expected the line to say the program was not found, got %q", line)
	}
}

func TestACommandThatOutlivesItsLimitIsStoppedAndReportedAsAFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "finished")
	// The shell starts sleep as a child and does not exec it, as a make would
	// start a compiler: stopping the shell alone would leave the sleep running.
	after := rig.NewAfterLanding(
		rig.WithAfterLimit(200*time.Millisecond),
		rig.WithAfterCommands(map[string]string{"millwright": "echo started; sleep 30; touch " + marker}),
	)

	began := time.Now()
	ran, err := after.Run(context.Background(), "millwright", t.TempDir())
	took := time.Since(began)

	if err != nil {
		t.Fatalf("expected a command that took too long to be reported, not to fail the run: %v", err)
	}
	if took > 10*time.Second {
		t.Errorf("expected the command to be stopped at its limit, it ran for %s", took)
	}
	if ran.Succeeded() || ran.TimedOut != 200*time.Millisecond || !strings.Contains(ran.Output, "started") {
		t.Errorf("expected a stopped run that says its limit and keeps what it printed, got %+v", ran)
	}
	if line := ran.Line(); !strings.Contains(line, "stopped after 200ms, still running") {
		t.Errorf("expected the line to say it was stopped, got %q", line)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("expected the command to be stopped before it finished")
	}
}

func TestACommandIsGivenFiveMinutesUnlessTheHostIsToldOtherwise(t *testing.T) {
	if application.AfterLandingLimit != 5*time.Minute {
		t.Errorf("expected the limit to be five minutes, got %s", application.AfterLandingLimit)
	}
}

func TestAnAfterLandingCommandThatCannotBeRunAtAllIsAnError(t *testing.T) {
	after := rig.NewAfterLanding(rig.WithAfterCommands(map[string]string{"millwright": "true"}))

	if _, err := after.Run(context.Background(), "millwright", "/no/such/checkout"); err == nil {
		t.Error("expected a checkout that is not there to fail the run")
	}
	if _, err := after.Run(context.Background(), "fellowship", t.TempDir()); err == nil {
		t.Error("expected a rig with no command to fail the run rather than pass it")
	}
}

func TestTheLineAboutARunQuotesTheTailOfItsOutputOnOneLine(t *testing.T) {
	ran := application.Ran{Command: "make build", Status: 2, Output: "one\ntwo\nthree\nfour\n\n"}
	if got, want := ran.Line(), "after landing: make build: exit status 2: two / three / four"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if got, want := (application.Ran{Command: "make build"}).Line(), "after landing: make build: ok"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	long := application.Ran{Command: "c", Status: 1, Output: strings.Repeat("x", 1000)}
	if line := long.Line(); len(line) > 300 {
		t.Errorf("expected a long output to be cut to a line, got %d bytes", len(line))
	}
}
