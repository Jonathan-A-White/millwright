package apptest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func helloSpec() application.SessionSpec {
	return application.SessionSpec{
		Name:    application.SessionName("mw-gq6.4"),
		Dir:     "/root/.mw-worktrees/mw-gq6.4",
		Env:     map[string]string{"MW_STORY": "mw-gq6.4"},
		Command: []string{"sh", "-c", "echo hello; sleep 1"},
	}
}

func startedRunner(t *testing.T) (*apptest.FakeRunner, application.SessionSpec) {
	t.Helper()
	runner := apptest.NewFakeRunner()
	spec := helloSpec()
	if err := runner.Start(context.Background(), spec); err != nil {
		t.Fatalf("starting %s: %v", spec.Name, err)
	}
	return runner, spec
}

func TestFakeRunnerStartsASessionAndRemembersHowItWasAsked(t *testing.T) {
	runner, spec := startedRunner(t)

	started, ok := runner.Spec(spec.Name)
	if !ok {
		t.Fatalf("expected the fake to remember %s", spec.Name)
	}
	if started.Dir != spec.Dir || started.Env["MW_STORY"] != "mw-gq6.4" {
		t.Errorf("expected the session's directory and environment, got %+v", started)
	}

	status, err := runner.Status(context.Background(), spec.Name)
	if err != nil {
		t.Fatalf("reading the status of %s: %v", spec.Name, err)
	}
	if !status.Running() {
		t.Errorf("expected a fresh session to be running, got %+v", status)
	}
}

func TestFakeRunnerRefusesASecondSessionOfTheSameName(t *testing.T) {
	runner, spec := startedRunner(t)
	if err := runner.Start(context.Background(), spec); err == nil {
		t.Fatal("expected starting a second session of the same name to fail")
	}
}

func TestFakeRunnerRefusesASpecItCouldNotStart(t *testing.T) {
	runner := apptest.NewFakeRunner()
	err := runner.Start(context.Background(), application.SessionSpec{Name: "mw-gq6.4", Command: []string{"sh"}})
	if err == nil {
		t.Fatal("expected a session name with a dot in it to be refused")
	}
}

func TestFakeRunnerReadsBackTheRecentOutput(t *testing.T) {
	runner, spec := startedRunner(t)
	runner.Write(spec.Name, "hello\n")
	runner.Write(spec.Name, "there\n")

	out, err := runner.Output(context.Background(), spec.Name, 10)
	if err != nil {
		t.Fatalf("reading the output of %s: %v", spec.Name, err)
	}
	if out != "hello\nthere" {
		t.Errorf("expected both lines, got %q", out)
	}

	out, err = runner.Output(context.Background(), spec.Name, 1)
	if err != nil {
		t.Fatalf("reading the last line of %s: %v", spec.Name, err)
	}
	if out != "there" {
		t.Errorf("expected only the last line, got %q", out)
	}
}

func TestFakeRunnerKeepsWhatWasSentToASession(t *testing.T) {
	runner, spec := startedRunner(t)
	if err := runner.Send(context.Background(), spec.Name, "carry on\n"); err != nil {
		t.Fatalf("sending to %s: %v", spec.Name, err)
	}
	sent := runner.Input(spec.Name)
	if len(sent) != 1 || sent[0] != "carry on\n" {
		t.Errorf("expected the input to be kept, got %q", sent)
	}
}

func TestFakeRunnerWaitsForACommandToExitAndKeepsItsStatus(t *testing.T) {
	runner, spec := startedRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		runner.Write(spec.Name, "done\n")
		runner.Exit(spec.Name, 3)
	}()

	status, err := runner.Wait(ctx, spec.Name)
	if err != nil {
		t.Fatalf("waiting for %s: %v", spec.Name, err)
	}
	if !status.Finished() || status.ExitCode != 3 {
		t.Fatalf("expected the session to have exited 3, got %+v", status)
	}

	// The output of a session that has exited is still readable: that is what
	// the exit status is judged alongside.
	out, err := runner.Output(ctx, spec.Name, 10)
	if err != nil {
		t.Fatalf("reading the output of the finished %s: %v", spec.Name, err)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("expected the output to survive the exit, got %q", out)
	}
}

func TestFakeRunnerWaitGivesUpWhenTheContextDoes(t *testing.T) {
	runner, spec := startedRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := runner.Wait(ctx, spec.Name); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected waiting to give up with the context, got %v", err)
	}
}

func TestFakeRunnerClosesASessionAndForgetsIt(t *testing.T) {
	runner, spec := startedRunner(t)
	ctx := context.Background()

	if err := runner.Close(ctx, spec.Name); err != nil {
		t.Fatalf("closing %s: %v", spec.Name, err)
	}
	status, err := runner.Status(ctx, spec.Name)
	if err != nil {
		t.Fatalf("reading the status of the closed %s: %v", spec.Name, err)
	}
	if status.State != application.StateGone {
		t.Errorf("expected a closed session to be gone, got %+v", status)
	}
	// Closing a session that is already gone is harmless.
	if err := runner.Close(ctx, spec.Name); err != nil {
		t.Errorf("expected closing a gone session to be harmless, got %v", err)
	}
}

func TestFakeRunnerRefusesToWorkOnASessionItDoesNotHave(t *testing.T) {
	runner := apptest.NewFakeRunner()
	ctx := context.Background()

	if err := runner.Send(ctx, "mw-nope", "hello\n"); err == nil {
		t.Error("expected sending to an unknown session to fail")
	}
	if _, err := runner.Output(ctx, "mw-nope", 10); err == nil {
		t.Error("expected reading an unknown session to fail")
	}
	status, err := runner.Status(ctx, "mw-nope")
	if err != nil {
		t.Fatalf("reading the status of an unknown session: %v", err)
	}
	if status.State != application.StateGone {
		t.Errorf("expected an unknown session to be gone, got %+v", status)
	}
}

func TestFakeRunnerFailsEverythingWhenItIsToldTo(t *testing.T) {
	runner, spec := startedRunner(t)
	runner.Err = errors.New("the runner is down")

	if _, err := runner.Status(context.Background(), spec.Name); err == nil {
		t.Error("expected the set error to be returned")
	}
}
