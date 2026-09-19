package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These run mw seat reap against a tmux server that is not there: the socket
// is named for this test alone, so nothing here can reach the default server,
// which holds the person's windows.
func reapCmd(t *testing.T, vault string, args ...string) (string, error) {
	t.Helper()
	mwConfig(t, "vault = \""+vault+"\"\nhost = \"laptop\"\n")
	t.Setenv(TmuxSocketEnv, "mw-test-no-such-server-"+strings.ReplaceAll(t.Name(), "/", "-"))
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"seat", "reap", "mayor"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestSeatReapNamesAWindowOrIsRefused(t *testing.T) {
	vault := t.TempDir()
	out, err := reapCmd(t, vault)
	if err == nil || !strings.Contains(err.Error(), "window") {
		t.Fatalf("expected a refusal naming the window, got %q, %v", out, err)
	}
	if _, statErr := os.Stat(filepath.Join(vault, ".mayor-reaper.log")); statErr == nil {
		t.Errorf("expected a refused watch to log nothing")
	}
}

func TestSeatReapLogsAWindowThatIsAlreadyGone(t *testing.T) {
	vault := t.TempDir()
	out, err := reapCmd(t, vault, "--window", "@9", "--interval", "10ms", "--limit", "5s")
	if err != nil {
		t.Fatalf("expected a window that is gone to end the watch cleanly, got %v", err)
	}
	if !strings.Contains(out, "reap @9: the window is already gone") {
		t.Errorf("expected the watch to say the window is gone, got %q", out)
	}
	logged, err := os.ReadFile(filepath.Join(vault, ".mayor-reaper.log"))
	if err != nil {
		t.Fatalf("reading the reaper log: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(logged)), "\n"); len(lines) != 2 {
		t.Errorf("expected an armed line and an outcome line, got:\n%s", logged)
	}
}

func TestSeatReapGivesUpWithAnErrorSoTheCommandExitsNonZero(t *testing.T) {
	// The server is not there, so the window is gone at the first look; to give
	// up the watch must be out of time before it looks, which a limit shorter
	// than an interval does.
	vault := t.TempDir()
	out, err := reapCmd(t, vault, "--window", "@9", "--interval", "50ms", "--limit", "1ns")
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("expected the watch to give up, got %q, %v", out, err)
	}
}
