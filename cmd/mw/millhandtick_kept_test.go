package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// aTmuxOfItsOwn leaves the sweep's look at the sessions on this host with a
// tmux server of the test's own, so that a host running story sessions does not
// add a bd to the tick for each of them. A short directory: a socket's path is
// limited to about a hundred characters.
func aTmuxOfItsOwn(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mwtick")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
}

// mw-gq6.1514: mw millhand tick built its Gateway with mwGateway and never kept
// a beads connection, so each tracker read in a tick started its own bd (11 a
// tick with the Millhand not up, every few minutes), and each bd costs the
// server several new connections: the largest single source of the beads-churn
// alarm. It reads over one connection kept open for the run, as mw next does.
//
// The tidy's open mail and open formulas (OpenMail, OpenFormulas) are read over
// the kept connection as well (mw-gq6.1544): they were the two bd a tick still
// started, one each.
func TestTheMillhandTickStartsNoBd(t *testing.T) {
	aTmuxOfItsOwn(t)
	privateTmux(t, "scratch")
	asked := aQuietBd(t)
	server := aServerToReach(t)
	server.empty.Store(true)

	out, err := millhandCmd(t, "tick")
	if err != nil {
		t.Fatalf("expected the tick to complete, got %q, %v\n%s", out, err, readAsked(t, asked))
	}
	if got := strings.TrimSpace(readAsked(t, asked)); got != "" {
		t.Errorf("expected no bd started by a tick, got %d:\n%s", len(strings.Split(got, "\n")), got)
	}
	if got := server.queries("from"); got == 0 {
		t.Errorf("expected the tick's reads to go over the kept connection, the server was asked nothing; the tick said:\n%s", out)
	}
}

// The sweep of the tick reads the story of each story session on the host with
// one ShowBeads, and that started a bd show per tick whenever a session ran,
// not served over the kept connection (mw-gq6.1545). It is read over the kept
// connection by id; the idle session of the closed story is closed all the
// same, and the sessions of the open ones are left.
func TestTheMillhandTickReadsSessionStoriesOverTheKeptConnection(t *testing.T) {
	aTmuxOfItsOwn(t)
	privateTmux(t, "scratch")
	for _, name := range []string{"mw-t_1", "mw-t_2", "mw-t_3"} {
		// The default server of this test's own TMUX_TMPDIR: where the sweep looks.
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "sh").CombinedOutput(); err != nil {
			t.Fatalf("starting the session %s: %v: %s", name, err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })
	asked := aQuietBd(t)
	server := aServerToReach(t)
	server.empty.Store(true)

	out, err := millhandCmd(t, "tick")
	if err != nil {
		t.Fatalf("expected the tick to complete, got %q, %v\n%s", out, err, readAsked(t, asked))
	}
	if got := strings.TrimSpace(readAsked(t, asked)); got != "" {
		t.Errorf("expected no bd started by a tick over three story sessions, got %d:\n%s", len(strings.Split(got, "\n")), got)
	}
	if got := server.queries("where i.id in ('"); got == 0 {
		t.Errorf("expected the stories of the sessions to be read by id over the kept connection, the server was asked no such query; the tick said:\n%s", out)
	}
	live, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		t.Fatalf("listing the sessions: %v", err)
	}
	if got := strings.Fields(string(live)); strings.Join(got, " ") != "mw-t_2 mw-t_3" {
		t.Errorf("expected the idle session of the closed story to be closed and the others left, got %v", got)
	}
}

// With no server to reach the tick reads by bd as it did, and completes.
func TestTheMillhandTickReadsByBdWhenThereIsNoServerToReach(t *testing.T) {
	aTmuxOfItsOwn(t)
	privateTmux(t, "scratch")
	asked := aQuietBd(t)

	out, err := millhandCmd(t, "tick")
	if err != nil {
		t.Fatalf("expected the tick to complete, got %q, %v\n%s", out, err, readAsked(t, asked))
	}
	if got := bdsAsked(t, asked); got == 0 {
		t.Errorf("expected the tick to read by bd with no server to reach, got no bd started; the tick said:\n%s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected the tick to say its line")
	}
}

// aQuietBd puts a bd on the PATH that records what it was asked and answers
// every read with nothing: a vault with no mail and no story in it.
func aQuietBd(t *testing.T) (asked string) {
	t.Helper()
	asked = aBdThatRecords(t)
	bin := filepath.Dir(mustLookBd(t))
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + asked + "\ncase \"$*\" in\n*\" kv \"*) echo '{}' ;;\n*) echo '[]' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return asked
}

func mustLookBd(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("bd")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
