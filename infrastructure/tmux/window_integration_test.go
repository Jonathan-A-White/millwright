package tmux_test

// This test drives a real tmux and opens real windows in it. Like the runner's
// integration test it never touches tmux's default server — the one holding
// whatever a person is doing on this machine, and the Mayor's own window with
// it. Every command goes to a private server of this test's own (tmux.WithSocket)
// and into a session of its own (tmux.WithSession), and what runs in the window
// is a stand-in script, never a real claude. Nothing here may be changed to run
// without both.

import (
	"context"
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

// privateWindows returns a Windows onto a tmux server and a session of this
// test's own, and kills that server when the test ends.
func privateWindows(t *testing.T) (*tmux.Windows, string) {
	t.Helper()
	if !tmux.Available() {
		t.Skipf("%s is not on PATH", tmux.Program)
	}

	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		// Killing the server takes the test's windows with it. It fails when the
		// server has already stopped, which is the happy ending.
		_ = exec.Command(tmux.Program, "-L", socket, "kill-server").Run()
		if err := exec.Command(tmux.Program, "-L", socket, "list-sessions").Run(); err == nil {
			t.Errorf("the test's tmux server %s is still running", socket)
		}
		_ = os.Remove(socketPath(socket))
	})
	return tmux.NewWindows(tmux.WithSocket(socket), tmux.WithSession("mw-test-seats")), socket
}

// standInSeat writes a script that stands in for claude: it records the seat,
// the directory and the arguments it was started with, then waits to be closed,
// as a session a person is typing into would.
func standInSeat(t *testing.T, dir string) (program, said string) {
	t.Helper()
	program = filepath.Join(dir, "claude-stand-in")
	said = filepath.Join(dir, "said")
	script := "#!/bin/sh\n" +
		"{ echo \"seat=$MW_SEAT\"; echo \"dir=$PWD\"; for arg in \"$@\"; do echo \"arg=$arg\"; done; } > " + said + "\n" +
		"sleep 60\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, said
}

// waitFor gives something a moment to happen: tmux starts the window's command
// itself, so there is a gap between Open returning and the command having run.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited for %s and it never happened", what)
}

func TestOpenStartsAWindowThatIsThereUntilItIsClosed(t *testing.T) {
	windows, socket := privateWindows(t)
	ctx := context.Background()

	// Nothing is running yet: the session itself is not there, which is no
	// windows rather than a failure.
	open, err := windows.List(ctx)
	if err != nil {
		t.Fatalf("listing the windows of a session that is not there: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("expected no windows before anything was opened, got %+v", open)
	}

	dir := t.TempDir()
	program, said := standInSeat(t, dir)
	// A kickoff as the real one is: quotes, a dollar sign and a newline, none of
	// which may be read by a shell on the way to the session.
	kickoff := "Boot by 'procedures.md'.\nNewest handoff: seats/mayor/handoffs/2026-09-19-12.md $HOME"
	spec := application.WindowSpec{
		Name:    "mayor-2026-09-19-13",
		Dir:     dir,
		Env:     map[string]string{"MW_SEAT": "mayor"},
		Command: []string{program, "--name", "mayor-2026-09-19-13", kickoff},
	}

	opened := time.Now()
	if err := windows.Open(ctx, spec); err != nil {
		t.Fatalf("opening the window: %v", err)
	}

	// It is there, and dated: a window mw opened is newer than the moment
	// before it opened it, and not in the future.
	open, err = windows.List(ctx)
	if err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	window, there := windowNamed(open, spec.Name)
	if !there {
		t.Fatalf("expected the window %s to be open, got %+v", spec.Name, open)
	}
	if window.Opened.Before(opened.Add(-5*time.Second)) || window.Opened.After(time.Now().Add(time.Second)) {
		t.Errorf("expected the window to be dated around %s, got %s", opened.Format(time.RFC3339), window.Opened.Format(time.RFC3339))
	}

	// The stand-in ran with the seat, the directory and the arguments it was
	// given, the kickoff whole.
	waitFor(t, "the stand-in to say what it was started with", func() bool {
		_, err := os.Stat(said)
		return err == nil
	})
	started, err := os.ReadFile(said)
	if err != nil {
		t.Fatalf("reading what the stand-in said: %v", err)
	}
	for _, want := range []string{"seat=mayor", "dir=" + dir, "arg=--name", "arg=mayor-2026-09-19-13", "arg=" + kickoff} {
		if !strings.Contains(string(started), want) {
			t.Errorf("expected the stand-in to have been started with %q, it said:\n%s", want, started)
		}
	}

	// A second seat goes in a window beside the first, in the same session.
	second := spec
	second.Name = "millhand-2026-09-19-01"
	if err := windows.Open(ctx, second); err != nil {
		t.Fatalf("opening a second window: %v", err)
	}
	if open, err = windows.List(ctx); err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("expected both windows to be open in the one session, got %+v", open)
	}

	// Closed, it is gone — and the other one is left alone.
	kill := exec.Command(tmux.Program, "-L", socket, "kill-window", "-t", "=mw-test-seats:="+spec.Name)
	if out, err := kill.CombinedOutput(); err != nil {
		t.Fatalf("closing the window: %v\n%s", err, out)
	}
	waitFor(t, "the closed window to be gone", func() bool {
		open, err := windows.List(ctx)
		if err != nil {
			t.Fatalf("listing the windows: %v", err)
		}
		_, there := windowNamed(open, spec.Name)
		return !there
	})
	if open, err = windows.List(ctx); err != nil {
		t.Fatalf("listing the windows: %v", err)
	}
	if _, there := windowNamed(open, second.Name); !there {
		t.Errorf("expected %s to be left open, got %+v", second.Name, open)
	}
}

// windowNamed is the window of that name among those open.
func windowNamed(open []application.Window, name string) (application.Window, bool) {
	for _, window := range open {
		if window.Name == name {
			return window, true
		}
	}
	return application.Window{}, false
}
