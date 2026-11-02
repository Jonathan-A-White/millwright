package tmux_test

// Two Deputies were up at once on 2026-11-02 (mw-gq6.891): `mw deputy`, run from
// the Mayor's pane in tmux session 0, opened a window in session 0 and looked
// for one only there, and the spring, run outside tmux, looked only in mw-seats
// and opened a second. These tests drive a real tmux on a private server, as
// window_integration_test.go does, and hold the two halves of the cure: every
// seat's window is opened in mw-seats, whoever opens it and wherever from, and
// a seat's window in any session is seen.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

// tmuxOn runs a tmux command on the private server and returns what it printed.
func tmuxOn(t *testing.T, socket string, args ...string) string {
	t.Helper()
	out, err := exec.Command(tmux.Program, append([]string{"-L", socket}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestListSeesASeatsWindowOpenInAnotherTmuxSession(t *testing.T) {
	windows, socket := privateWindows(t)
	ctx := context.Background()

	// The Mayor's own session, named as tmux names the first: 0.
	tmuxOn(t, socket, "new-session", "-d", "-s", "0", "-n", "deputy-2026-11-02-888", "sleep", "60")

	open, err := windows.List(ctx)
	if err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	if _, there := windowNamed(open, "deputy-2026-11-02-888"); !there {
		t.Fatalf("expected the Deputy's window in session 0 to be seen from mw's own session, got %+v", open)
	}
}

func TestOpenFromInsideAnotherTmuxSessionOpensInTheSeatsSession(t *testing.T) {
	_, socket := privateWindows(t)
	ctx := context.Background()

	// Session 0 is the Mayor's pane, and mw runs in it: TMUX names that server and session.
	tmuxOn(t, socket, "new-session", "-d", "-s", "0", "sleep", "60")
	t.Setenv(tmux.TmuxEnv, tmuxOn(t, socket, "display-message", "-p", "-t", "0", "#{socket_path},#{pid},0"))

	windows := tmux.NewWindows(tmux.WithSocket(socket))
	spec := application.WindowSpec{
		Name:    "deputy-2026-11-02-888",
		Dir:     t.TempDir(),
		Command: []string{"sleep", "60"},
	}
	if err := windows.Open(ctx, spec); err != nil {
		t.Fatalf("opening the window: %v", err)
	}

	for session, want := range map[string]bool{tmux.SeatsSession: true, "0": false} {
		listed := tmuxOn(t, socket, "list-windows", "-t", "="+session, "-F", "#{window_name}")
		if got := strings.Contains(listed, spec.Name); got != want {
			t.Errorf("expected the window %s in session %s to be %v, session %s holds:\n%s", spec.Name, session, want, session, listed)
		}
	}
}

func TestASecondDeputyIsNotOpenedWhereTheFirstWasNotLooked(t *testing.T) {
	// The whole of the 2026-11-02 sequence, on the adapter: one Deputy opened
	// from inside session 0, then a look from outside tmux, as the spring's.
	_, socket := privateWindows(t)
	ctx := context.Background()
	tmuxOn(t, socket, "new-session", "-d", "-s", "0", "sleep", "60")
	t.Setenv(tmux.TmuxEnv, tmuxOn(t, socket, "display-message", "-p", "-t", "0", "#{socket_path},#{pid},0"))
	inside := tmux.NewWindows(tmux.WithSocket(socket))
	if err := inside.Open(ctx, application.WindowSpec{Name: "deputy-2026-11-02-888", Dir: t.TempDir(), Command: []string{"sleep", "60"}}); err != nil {
		t.Fatalf("opening the window: %v", err)
	}

	t.Setenv(tmux.TmuxEnv, "")
	_ = os.Unsetenv(tmux.TmuxEnv)
	outside := tmux.NewWindows(tmux.WithSocket(socket))
	open, err := outside.List(ctx)
	if err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	if _, there := windowNamed(open, "deputy-2026-11-02-888"); !there {
		t.Fatalf("expected the Deputy opened from inside tmux to be seen from outside it, got %+v", open)
	}
}
