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
	//
	// The limit is two seconds, not a fraction of one: the output must hold what
	// the command printed before it was stopped, and a loaded host can take more
	// than 200 ms to start the shell that prints it (mw-gq6.922).
	after := rig.NewAfterLanding(
		rig.WithAfterLimit(2*time.Second),
		rig.WithAfterCommands(map[string]string{"millwright": "echo started; sleep 30; touch " + marker}),
	)

	began := time.Now()
	ran, err := after.Run(context.Background(), "millwright", t.TempDir())
	took := time.Since(began)

	if err != nil {
		t.Fatalf("expected a command that took too long to be reported, not to fail the run: %v", err)
	}
	if took > 12*time.Second {
		t.Errorf("expected the command to be stopped at its limit, it ran for %s", took)
	}
	if ran.Succeeded() || ran.TimedOut != 4*time.Second || !strings.Contains(ran.Output, "started") {
		t.Errorf("expected a stopped run that says its limit and keeps what it printed, got %+v", ran)
	}
	if line := ran.Line(); !strings.Contains(line, "stopped after 4s, still running") {
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

func TestARigsOwnLimitReplacesTheDefaultForThatRigAlone(t *testing.T) {
	// The default is short, as 5m is next to a slow uplink; the rig that names a
	// longer limit gets it and another rig keeps the default. The margins are
	// tenfold or more each way, so that a busy host cannot move a command across
	// a limit (mw-gq6.423): the 0.5s command is ten times the 50ms default and a
	// twentieth of the 10s limit, and the 30s command is far past the default.
	after := rig.NewAfterLanding(
		rig.WithAfterLimit(50*time.Millisecond),
		rig.WithAfterLimits(map[string]time.Duration{"postern": 10 * time.Second}),
		rig.WithAfterCommands(map[string]string{"postern": "sleep 0.5; echo deployed", "millwright": "sleep 30; echo built"}),
	)

	ran, err := after.Run(context.Background(), "postern", t.TempDir())
	if err != nil || !ran.Succeeded() || !strings.Contains(ran.Output, "deployed") {
		t.Errorf("expected the rig with a 10s limit to run its 0.5s command to the end, got %+v, %v", ran, err)
	}
	ran, err = after.Run(context.Background(), "millwright", t.TempDir())
	if err != nil || ran.TimedOut != 100*time.Millisecond {
		t.Errorf("expected a rig with no limit of its own to be stopped at the default 50ms stretched, 100ms, got %+v, %v", ran, err)
	}
}

func TestTheLimitOfARigIsItsOwnOrTheDefault(t *testing.T) {
	after := rig.NewAfterLanding(rig.WithAfterLimits(map[string]time.Duration{"postern": 20 * time.Minute, "zero": 0, "negative": -time.Minute}))

	if got := after.Limit("postern"); got != 20*time.Minute {
		t.Errorf("expected postern's 20m, got %s", got)
	}
	for _, name := range []string{"millwright", "zero", "negative"} {
		if got := after.Limit(name); got != 5*time.Minute {
			t.Errorf("expected %s to keep the default five minutes, got %s", name, got)
		}
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

// A line written to the config while a batch runs is the one that runs
// (mw-gq6.1001): the command is read when the step runs, not when the batch began.
func TestTheAfterLandingCommandIsReadWhenTheStepRunsAndNotWhenTheBatchBegan(t *testing.T) {
	line := "echo old"
	read := func() (map[string]string, error) { return map[string]string{"lampas": line}, nil }
	at := time.Date(2026, 11, 5, 23, 28, 0, 0, time.UTC)
	after := rig.NewAfterLanding(
		rig.WithAfterCommands(map[string]string{"lampas": "echo old"}),
		rig.WithAfterCommandsFrom(read, func() time.Time { return at }),
	)

	line = "echo new"
	ran, err := after.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatalf("running the command: %v", err)
	}
	if ran.Command != "echo new" || !strings.Contains(ran.Output, "new") {
		t.Errorf("expected the line written during the batch to run, got %+v", ran)
	}
	if !ran.Changed || ran.ReadAt != at {
		t.Errorf("expected the run to say it was read at %s and differs from the batch's line, got changed=%v at=%s", at, ran.Changed, ran.ReadAt)
	}
	if got := after.Command("lampas"); got != "echo new" {
		t.Errorf("expected Command to read the config afresh, got %q", got)
	}
}

func TestARunWhoseLineIsTheOneTheBatchBeganWithIsNotChanged(t *testing.T) {
	read := func() (map[string]string, error) { return map[string]string{"lampas": "echo same"}, nil }
	after := rig.NewAfterLanding(
		rig.WithAfterCommands(map[string]string{"lampas": "echo same"}),
		rig.WithAfterCommandsFrom(read, time.Now),
	)

	ran, err := after.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatalf("running the command: %v", err)
	}
	if ran.Changed || ran.ReadAt.IsZero() {
		t.Errorf("expected an unchanged line read just now, got changed=%v at=%s", ran.Changed, ran.ReadAt)
	}
}

func TestAConfigThatCannotBeReadAtTheStepRunsTheLineTheBatchBeganWithAndSaysSo(t *testing.T) {
	read := func() (map[string]string, error) { return nil, os.ErrPermission }
	after := rig.NewAfterLanding(
		rig.WithAfterCommands(map[string]string{"lampas": "echo began"}),
		rig.WithAfterCommandsFrom(read, time.Now),
	)

	ran, err := after.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatalf("running the command: %v", err)
	}
	if ran.Command != "echo began" || ran.ReadFailed == "" || ran.Changed {
		t.Errorf("expected the batch's own line and the failure of the read said, got %+v", ran)
	}
}

// The limit of a rig is read from the host's config ([after_landing_limit], through
// WithAfterLimits), not sized from earlier runs: a command that finishes after it
// is waited for, up to a stretch of it (mw-gq6.1268).
func TestACommandThatFinishesAfterItsLimitIsWaitedForAndNotSaidToBeStopped(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "finished")
	// The limit is 0.5s and the stretch twenty times that, so the command's 2s is
	// four times the limit and a fifth of the point it is stopped at: a busy host
	// cannot move it across either (mw-gq6.423).
	after := rig.NewAfterLanding(
		rig.WithAfterLimits(map[string]time.Duration{"lampas": 500 * time.Millisecond}),
		rig.WithAfterStretch(20),
		rig.WithAfterCommands(map[string]string{"lampas": "echo shipping; sleep 2; touch " + marker + "; echo shipped"}),
	)

	ran, err := after.Run(context.Background(), "lampas", t.TempDir())

	if err != nil {
		t.Fatalf("expected a late command to be reported, not to fail the run: %v", err)
	}
	if !ran.Succeeded() || ran.TimedOut != 0 || !strings.Contains(ran.Output, "shipped") {
		t.Errorf("expected the command to be waited for to its end, got %+v", ran)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("expected the command to have finished: %v", err)
	}
	if ran.Limit != 500*time.Millisecond || ran.Took < 2*time.Second {
		t.Errorf("expected the run to say its limit and how long it took, got limit %s, took %s", ran.Limit, ran.Took)
	}
	line := ran.Line()
	if strings.Contains(line, "half-deployed") || !strings.Contains(line, "finished by itself") || !strings.Contains(line, "past its limit of 500ms") {
		t.Errorf("expected the line to say it ran past its limit and finished by itself, got %q", line)
	}
}

func TestACommandThatNeverFinishesIsStillStoppedAtTheStretchedLimit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "finished")
	after := rig.NewAfterLanding(
		rig.WithAfterLimits(map[string]time.Duration{"lampas": 500 * time.Millisecond}),
		rig.WithAfterStretch(4),
		rig.WithAfterCommands(map[string]string{"lampas": "echo started; sleep 30; touch " + marker}),
	)

	began := time.Now()
	ran, err := after.Run(context.Background(), "lampas", t.TempDir())
	took := time.Since(began)

	if err != nil {
		t.Fatalf("expected a hung command to be reported, not to fail the run: %v", err)
	}
	if took > 15*time.Second {
		t.Errorf("expected the command to be stopped, it ran for %s", took)
	}
	if ran.Succeeded() || ran.TimedOut != 2*time.Second || ran.Limit != 500*time.Millisecond {
		t.Errorf("expected a stopped run that says where it was stopped (4 x 500ms) and its limit, got %+v", ran)
	}
	if line := ran.Line(); !strings.Contains(line, "stopped after 2s, still running") {
		t.Errorf("expected the line to say it was stopped, got %q", line)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("expected the command to be stopped before it finished")
	}
}

func TestACommandThatFailsAfterItsLimitSaysSoAndKeepsItsStatus(t *testing.T) {
	after := rig.NewAfterLanding(
		rig.WithAfterLimits(map[string]time.Duration{"lampas": 500 * time.Millisecond}),
		rig.WithAfterStretch(20),
		rig.WithAfterCommands(map[string]string{"lampas": "sleep 2; echo rsync broke; exit 3"}),
	)

	ran, err := after.Run(context.Background(), "lampas", t.TempDir())

	if err != nil || ran.Status != 3 || ran.TimedOut != 0 {
		t.Fatalf("expected the command's own exit status 3, got %+v, %v", ran, err)
	}
	if line := ran.Line(); strings.Contains(line, "half-deployed") || !strings.Contains(line, "exit status 3") || !strings.Contains(line, "past its limit of 500ms") {
		t.Errorf("expected the line to say its status and that it ran past its limit, got %q", line)
	}
}

func TestACommandIsStoppedAtTwiceItsLimitUnlessTheStretchIsChanged(t *testing.T) {
	if application.AfterLandingStretch != 2 {
		t.Errorf("expected the stretch to be 2, got %d", application.AfterLandingStretch)
	}
}
