package tmux_test

// This test drives a real tmux. It never touches tmux's default server, the one
// that holds whatever a person is doing on this machine: every command goes to
// a private server of its own, named by a socket the test makes up and kills on
// the way out. That is what tmux.WithSocket is for, and nothing here may be
// changed to run without it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

// privateRunner returns a Runner onto a tmux server of this test's own, and
// kills that server when the test ends.
func privateRunner(t *testing.T) *tmux.Runner {
	t.Helper()
	if !tmux.Available() {
		t.Skipf("%s is not on PATH", tmux.Program)
	}

	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		// kill-server fails when the server has already stopped, which is the
		// happy ending: tmux stops on its own once its last session is closed.
		_ = exec.Command(tmux.Program, "-L", socket, "kill-server").Run()
		if err := exec.Command(tmux.Program, "-L", socket, "list-sessions").Run(); err == nil {
			t.Errorf("the test's tmux server %s is still running", socket)
		}
		_ = os.Remove(socketPath(socket))
	})
	return tmux.New(tmux.WithSocket(socket), tmux.WithPollInterval(20*time.Millisecond))
}

// expectExit checks how a command that has ended is reported: with the status it
// exited with, or as ended with its status unknown — never as any other status.
//
// Unknown has to be allowed because tmux does not always keep a command's status:
// pinned to one CPU it often reaps the command and never records how it ended,
// and then pane_dead_status stays empty for good. The runner gives up on that
// after a grace and says the exit is unknown, which is the answer to a status
// that never comes. What must never happen is a command that exited 3 reported
// as having exited 0, or Wait not returning at all.
func expectExit(t *testing.T, status application.SessionStatus, code int) {
	t.Helper()
	switch {
	case status.Finished() && status.ExitCode == code:
	case status.State == application.StateExitUnknown:
		t.Logf("tmux did not keep the exit status of %s (wanted %d): reported unknown", status.Name, code)
	default:
		t.Fatalf("expected %s to have exited %d or ended with its status unknown, got %+v", status.Name, code, status)
	}
}

// socketPath is where tmux puts the socket of a server named with -L, so that a
// test can take its own socket away with it.
func socketPath(socket string) string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), socket)
}

// sessionsOn is what tmux itself says is on the runner's server.
func sessionsOn(t *testing.T, runner *tmux.Runner) string {
	t.Helper()
	out, err := exec.Command(tmux.Program, "-L", runner.Socket(), "list-sessions").CombinedOutput()
	if err != nil && !strings.Contains(string(out), "no server running") {
		t.Fatalf("listing the sessions on %s: %v\n%s", runner.Socket(), err, out)
	}
	return string(out)
}

// waitForOutput reads a session until what it printed holds want.
func waitForOutput(ctx context.Context, t *testing.T, runner *tmux.Runner, name, want string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		out, err := runner.Output(ctx, name, 50)
		if err != nil {
			t.Fatalf("reading the output of %s: %v", name, err)
		}
		if strings.Contains(out, want) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %s to print %q; it printed:\n%s", name, want, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunnerRunsACommandToItsEndInTmux(t *testing.T) {
	runner := privateRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	dir := t.TempDir()
	name := application.SessionName("mw-gq6.4")
	if name != "mw-gq6_4" {
		t.Fatalf("expected the story id to become a tmux-legal name, got %q", name)
	}

	spec := application.SessionSpec{
		Name:    name,
		Dir:     dir,
		Command: []string{"sh", "-c", "echo hello; sleep 1"},
	}
	if err := runner.Start(ctx, spec); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}

	// It is running under the name the story gave it, and it prints as it goes.
	status, err := runner.Status(ctx, name)
	if err != nil {
		t.Fatalf("reading the status of %s: %v", name, err)
	}
	if !status.Running() {
		t.Fatalf("expected %s to be running, got %+v", name, status)
	}
	waitForOutput(ctx, t, runner, name, "hello")

	// Waiting reports the exit, and the output survives it.
	status, err = runner.Wait(ctx, name)
	if err != nil {
		t.Fatalf("waiting for %s: %v", name, err)
	}
	expectExit(t, status, 0)
	out, err := runner.Output(ctx, name, 50)
	if err != nil {
		t.Fatalf("reading the output of the finished %s: %v", name, err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected the output to survive the exit, got %q", out)
	}

	// Closing it leaves no session behind.
	if err := runner.Close(ctx, name); err != nil {
		t.Fatalf("closing %s: %v", name, err)
	}
	status, err = runner.Status(ctx, name)
	if err != nil {
		t.Fatalf("reading the status of the closed %s: %v", name, err)
	}
	if status.State != application.StateGone {
		t.Fatalf("expected %s to be gone, got %+v", name, status)
	}
	if listed := sessionsOn(t, runner); strings.Contains(listed, name) {
		t.Fatalf("expected no tmux session left behind, tmux lists:\n%s", listed)
	}
}

func TestRunnerCarriesTheDirectoryTheEnvironmentAndWhatIsTypedIn(t *testing.T) {
	runner := privateRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("in-the-worktree"), 0o600); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}

	name := application.SessionName("mw-gq6.4.1")
	spec := application.SessionSpec{
		Name:    name,
		Dir:     dir,
		Env:     map[string]string{"MW_STORY": "mw-gq6.4"},
		Command: []string{"sh", "-c", `cat marker; echo "story=$MW_STORY"; read line; echo "typed=$line"; exit 3`},
	}
	if err := runner.Start(ctx, spec); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}
	defer func() {
		if err := runner.Close(context.Background(), name); err != nil {
			t.Errorf("closing %s: %v", name, err)
		}
	}()

	// The command ran in the directory it was given, with the environment it
	// was given.
	waitForOutput(ctx, t, runner, name, "in-the-worktree")
	waitForOutput(ctx, t, runner, name, "story=mw-gq6.4")

	// What is sent reaches the command as if it were typed.
	if err := runner.Send(ctx, name, "carry on\n"); err != nil {
		t.Fatalf("sending to %s: %v", name, err)
	}
	waitForOutput(ctx, t, runner, name, "typed=carry on")

	// And its exit status is there to be read afterwards.
	status, err := runner.Wait(ctx, name)
	if err != nil {
		t.Fatalf("waiting for %s: %v", name, err)
	}
	expectExit(t, status, 3)
}

func TestRunnerWaitGivesUpWhenTheContextDoes(t *testing.T) {
	runner := privateRunner(t)
	name := application.SessionName("mw-gq6.4")
	if err := runner.Start(context.Background(), application.SessionSpec{Name: name, Command: []string{"sleep", "30"}}); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}
	defer func() {
		if err := runner.Close(context.Background(), name); err != nil {
			t.Errorf("closing %s: %v", name, err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	status, err := runner.Wait(ctx, name)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected waiting to give up with the context, got %v", err)
	}
	if !status.Running() {
		t.Errorf("expected the last status seen to be a running session, got %+v", status)
	}
}

func TestRunnerOnASessionThatIsNotThere(t *testing.T) {
	runner := privateRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A server with nothing on it at all: no session, and closing one is
	// harmless rather than an error.
	status, err := runner.Status(ctx, "mw-nope")
	if err != nil {
		t.Fatalf("reading the status of an unknown session: %v", err)
	}
	if status.State != application.StateGone {
		t.Fatalf("expected an unknown session to be gone, got %+v", status)
	}
	if err := runner.Close(ctx, "mw-nope"); err != nil {
		t.Errorf("expected closing an unknown session to be harmless, got %v", err)
	}
	if _, err := runner.Output(ctx, "mw-nope", 10); err == nil {
		t.Error("expected reading an unknown session to fail")
	}

	// And with a session running, an unknown name is still gone: tmux is
	// pointed at one session exactly, not at whatever matches.
	name := application.SessionName("mw-gq6.4")
	if err := runner.Start(ctx, application.SessionSpec{Name: name, Command: []string{"sleep", "30"}}); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}
	defer func() {
		if err := runner.Close(context.Background(), name); err != nil {
			t.Errorf("closing %s: %v", name, err)
		}
	}()
	status, err = runner.Status(ctx, "mw-gq6")
	if err != nil {
		t.Fatalf("reading the status of a name that is only a prefix: %v", err)
	}
	if status.State != application.StateGone {
		t.Fatalf("expected a prefix of a running session's name to be gone, got %+v", status)
	}

	// Starting a second session of the same name is refused.
	if err := runner.Start(ctx, application.SessionSpec{Name: name, Command: []string{"sleep", "30"}}); err == nil {
		t.Error("expected a second session of the same name to be refused")
	}
}
