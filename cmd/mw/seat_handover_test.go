package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// handoverOf runs mw seat handover with args on the private tmux server, from
// the pane of the window named, as the old Mayor does.
func handoverOf(t *testing.T, socket, window string, args ...string) (string, error) {
	t.Helper()
	pane, err := exec.Command("tmux", "-L", socket, "display-message", "-p", "-t", window, "#{pane_id}").Output()
	if err != nil {
		t.Fatalf("asking tmux for the pane of %s: %v", window, err)
	}
	t.Setenv("TMUX_PANE", strings.TrimSpace(string(pane)))
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"seat", "handover"}, args...))
	err = root.Execute()
	return out.String(), err
}

func TestSeatHandoverMarksTheLogNamesTheSuccessorAndEndsTheOldWaits(t *testing.T) {
	const oldWindow, newWindow = "mayor-2026-10-01-159", "mayor-2026-10-01-160"
	socket := privateTmux(t, oldWindow)
	if out, err := exec.Command("tmux", "-L", socket, "new-window", "-d", "-n", newWindow, "sleep 60").CombinedOutput(); err != nil {
		t.Fatalf("opening the successor's window: %v: %s", err, out)
	}
	vaultDir := t.TempDir()
	eventsHome(t)
	mwConfig(t, "vault = \""+vaultDir+"\"\nhost = \"laptop\"\n")
	for _, args := range [][]string{
		{"emit", "--kind", "job", "--from", "scheduled", "--to", "running"},
		{"emit", "--kind", "mail", "--bead", "mw-m1", "--detail", "mayor"},
	} {
		if out, err := runEvents(t, args...); err != nil {
			t.Fatalf("mw events %v: %v\n%s", args, err, out)
		}
	}

	out, err := handoverOf(t, socket, oldWindow, "--at", "2")
	if err != nil {
		t.Fatalf("mw seat handover: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Handed over to "+newWindow+" at event 2") {
		t.Errorf("printed %q", out)
	}
	acting, err := os.ReadFile(filepath.Join(vaultDir, ".mayor-acting"))
	if err != nil || !strings.Contains(string(acting), newWindow) || !strings.Contains(string(acting), "event 2") || strings.Contains(string(acting), oldWindow) {
		t.Fatalf("acting file %q, %v", acting, err)
	}
	tail, err := runEvents(t, "tail", "--since", "2")
	if err != nil || !strings.Contains(tail, " handover - mayor - mayor to "+newWindow+" at 2") {
		t.Fatalf("the log's new event: %q, %v", tail, err)
	}

	// The old window's wait, begun before the handover, ends on it.
	waited, err := runEvents(t, "wait", "--for", "mayor", "--as", oldWindow, "--since", "0", "--limit", "5s")
	if err != nil || !strings.Contains(waited, "handed over at 2") {
		t.Fatalf("the old window's wait: %q, %v", waited, err)
	}
	// The successor's, begun at N, ignores it: it runs to its limit.
	waited, err = runEvents(t, "wait", "--for", "mayor", "--as", newWindow, "--since", "2", "--limit", "1200ms")
	if err != nil || strings.Contains(waited, "handed over") || strings.Contains(waited, "hold the") || !strings.Contains(waited, "No events") {
		t.Fatalf("the successor's wait from N: %q, %v", waited, err)
	}
}

func TestSeatHandoverWithNoSuccessorUpIsRefused(t *testing.T) {
	socket := privateTmux(t, "mayor-2026-10-01-159")
	eventsHome(t)
	vaultDir := t.TempDir()
	mwConfig(t, "vault = \""+vaultDir+"\"\nhost = \"laptop\"\n")
	out, err := handoverOf(t, socket, "mayor-2026-10-01-159")
	if err == nil || !strings.Contains(err.Error(), "start the successor first") {
		t.Fatalf("got %q, %v", out, err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, ".mayor-acting")); statErr == nil {
		t.Error("an acting file was written")
	}
}
