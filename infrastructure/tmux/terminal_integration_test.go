package tmux_test

// This test drives a real tmux on a server of its own, as tmux_integration_test.go
// does, and never touches tmux's default server.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

// shellWait is how long a real shell is given to draw and run what is typed to
// it. A shell starting from a full profile on a host at its full load takes
// longer than 5 seconds (mw-gq6.894 was refused at load 15.8 of 16); the wait
// returns as soon as the output is there, so a long limit costs nothing at
// normal load.
const shellWait = 60 * time.Second

// screenHolding reads a terminal's screen until it holds want, or until limit
// passes; it returns the last screen read and whether it held want.
func screenHolding(ctx context.Context, t *testing.T, term *tmux.Terminal, name, want string, limit time.Duration) (string, bool) {
	t.Helper()
	var screen string
	for deadline := time.Now().Add(limit); ; time.Sleep(100 * time.Millisecond) {
		var err error
		if screen, err = term.Capture(ctx, name); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(screen, want) {
			return screen, true
		}
		if time.Now().After(deadline) {
			return screen, false
		}
	}
}

// withShell makes the shell a new tmux server starts a script of the test's own.
func withShell(t *testing.T, body string) {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
}

func openTerminal(t *testing.T) (*tmux.Terminal, string) {
	t.Helper()
	if !tmux.Available() {
		t.Skipf("%s is not on PATH", tmux.Program)
	}
	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), socketCount.Add(1))
	t.Cleanup(func() {
		_ = exec.Command(tmux.Program, "-L", socket, "kill-server").Run()
		_ = os.Remove(socketPath(socket))
	})
	term := tmux.NewTerminal(tmux.WithSocket(socket), tmux.WithCallDeadline(integrationCallDeadline))
	name := "mw-usage-probe-test"
	if err := term.Open(t.Context(), name, t.TempDir(), 120, 40); err != nil {
		t.Fatal(err)
	}
	return term, name
}

func typeTheProbe(t *testing.T, term *tmux.Terminal, name string) {
	t.Helper()
	if err := term.Type(t.Context(), name, "echo usage-probe-$((6*7))"); err != nil {
		t.Fatal(err)
	}
	if err := term.Press(t.Context(), name, "Enter"); err != nil {
		t.Fatal(err)
	}
}

func TestATerminalIsTypedIntoReadAndClosed(t *testing.T) {
	if !tmux.Available() {
		t.Skipf("%s is not on PATH", tmux.Program)
	}
	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), socketCount.Add(1))
	t.Cleanup(func() {
		_ = exec.Command(tmux.Program, "-L", socket, "kill-server").Run()
		_ = os.Remove(socketPath(socket))
	})
	term := tmux.NewTerminal(tmux.WithSocket(socket), tmux.WithCallDeadline(integrationCallDeadline))
	ctx := t.Context()
	name := "mw-usage-probe-test"
	if probes, err := term.Probes(ctx); err != nil || len(probes) != 0 {
		t.Fatalf("probes before any server = %v, %v; want none", probes, err)
	}
	if err := term.Open(ctx, name, t.TempDir(), 120, 40); err != nil {
		t.Fatal(err)
	}
	probes, err := term.Probes(ctx)
	if err != nil || len(probes) != 1 || probes[0].Name != name || time.Since(probes[0].Created) > time.Minute {
		t.Fatalf("probes = %v, %v; want the one just opened", probes, err)
	}
	typeTheProbe(t, term, name)
	if screen, ok := screenHolding(ctx, t, term, name, "usage-probe-42", shellWait); !ok {
		t.Fatalf("the typed command's output is not on the screen:\n%s", screen)
	}
	if err := term.Close(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := term.Close(ctx, name); err != nil {
		t.Fatalf("closing a terminal already gone: %v", err)
	}
}

// A shell that is slow to start (a full profile on a loaded host) still runs
// what was typed to it before it was ready, within the wait. The old wait was 5
// seconds (mw-gq6.897).
func TestATerminalWhoseShellIsSlowToStartStillShowsTheOutput(t *testing.T) {
	withShell(t, "sleep 7\nexec /bin/sh")
	term, name := openTerminal(t)
	typeTheProbe(t, term, name)
	if screen, ok := screenHolding(t.Context(), t, term, name, "usage-probe-42", shellWait); !ok {
		t.Fatalf("the typed command's output is not on the screen after a slow start:\n%s", screen)
	}
}

// A shell that never prints is still a failure, with the screen to show for it.
func TestAShellThatNeverPrintsLeavesTheOutputOffTheScreen(t *testing.T) {
	withShell(t, "exec sleep 600")
	term, name := openTerminal(t)
	typeTheProbe(t, term, name)
	screen, ok := screenHolding(t.Context(), t, term, name, "usage-probe-42", time.Second)
	if ok {
		t.Fatalf("expected a shell that never prints to show no output, the screen was:\n%s", screen)
	}
}
