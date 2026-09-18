package tmux

// The unit tests here cover the parts that are ours rather than tmux's: the
// arguments a session is started with, the targets tmux is pointed at, and the
// reading of what it prints back. tmux itself is exercised by the integration
// test, which runs against a private tmux server.

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestStartArgsCarryTheSessionAndKeepTheWindowAfterTheCommandExits(t *testing.T) {
	spec := application.SessionSpec{
		Name:    "mw-gq6_4",
		Dir:     "/root/.mw-worktrees/mw-gq6.4",
		Env:     map[string]string{"MW_STORY": "mw-gq6.4", "MW_SEAT": "builder"},
		Command: []string{"sh", "-c", "echo hello; sleep 1"},
	}
	got := strings.Join(startArgs(spec), " ")
	want := "new-session -d -s mw-gq6_4 -c /root/.mw-worktrees/mw-gq6.4 " +
		"-e MW_SEAT=builder -e MW_STORY=mw-gq6.4 " +
		"sh -c echo hello; sleep 1 " +
		"; set-option -t =mw-gq6_4: remain-on-exit on"
	if got != want {
		t.Errorf("startArgs:\n got %s\nwant %s", got, want)
	}
}

func TestStartArgsLeaveOutWhatTheSessionDoesNotAskFor(t *testing.T) {
	spec := application.SessionSpec{Name: "mw-gq6_4", Command: []string{"sh"}}
	got := strings.Join(startArgs(spec), " ")
	want := "new-session -d -s mw-gq6_4 sh ; set-option -t =mw-gq6_4: remain-on-exit on"
	if got != want {
		t.Errorf("startArgs:\n got %s\nwant %s", got, want)
	}
}

func TestTargetsNameOneSessionExactly(t *testing.T) {
	// tmux would read a bare name as a pattern; the leading = makes it an exact
	// match, and the trailing colon says "that session's current window".
	if got := target("mw-gq6_4"); got != "=mw-gq6_4:" {
		t.Errorf("target = %q, want %q", got, "=mw-gq6_4:")
	}
	if got := sessionTarget("mw-gq6_4"); got != "=mw-gq6_4" {
		t.Errorf("sessionTarget = %q, want %q", got, "=mw-gq6_4")
	}
}

func TestParseStatusReadsWhatTheCommandIsDoing(t *testing.T) {
	cases := []struct {
		why      string
		printed  string
		state    application.SessionState
		exitCode int
	}{
		{"a live pane", "0|\n", application.StateRunning, 0},
		{"a command that exited well", "1|0\n", application.StateExited, 0},
		{"a command that failed", "1|7\n", application.StateExited, 7},
		{"a dead pane tmux has no status for", "1|\n", application.StateExited, 0},
		{"a window with a second pane in it", "0|\n0|\n", application.StateRunning, 0},
	}
	for _, c := range cases {
		status, err := parseStatus("mw-gq6_4", []byte(c.printed))
		if err != nil {
			t.Errorf("%s: %v", c.why, err)
			continue
		}
		if status.Name != "mw-gq6_4" || status.State != c.state || status.ExitCode != c.exitCode {
			t.Errorf("%s: parseStatus(%q) = %+v, want state %q code %d", c.why, c.printed, status, c.state, c.exitCode)
		}
	}
}

func TestParseStatusRefusesWhatItCannotRead(t *testing.T) {
	for _, printed := range []string{"", "\n", "yes|0\n", "1|later\n"} {
		if _, err := parseStatus("mw-gq6_4", []byte(printed)); err == nil {
			t.Errorf("expected %q to be refused", printed)
		}
	}
}

func TestStartRefusesACommandTmuxWouldReadAsASecondCommand(t *testing.T) {
	// tmux splits its own arguments on ";", so a command holding one would be
	// cut in half rather than run.
	spec := application.SessionSpec{Name: "mw-gq6_4", Command: []string{"sh", ";", "-c"}}
	err := New(WithSocket("mw-test-never-started")).Start(t.Context(), spec)
	if err == nil {
		t.Fatal("expected a command holding a bare semicolon to be refused")
	}
	if !strings.Contains(err.Error(), ";") {
		t.Errorf("expected the refusal to say what is wrong with the command, got %q", err)
	}
}

func TestSocketReportsTheServerTheRunnerWorksOn(t *testing.T) {
	if socket := New().Socket(); socket != "" {
		t.Errorf("expected a runner with no socket to work on tmux's default server, got %q", socket)
	}
	if socket := New(WithSocket("mw-test-1")).Socket(); socket != "mw-test-1" {
		t.Errorf("expected the socket the runner was built with, got %q", socket)
	}
}

func TestCommandArgsPutTheSocketBeforeTheCommand(t *testing.T) {
	got := strings.Join(New(WithSocket("mw-test-1")).commandArgs("kill-session", "-t", "=mw-gq6_4"), " ")
	if want := "-L mw-test-1 kill-session -t =mw-gq6_4"; got != want {
		t.Errorf("commandArgs = %q, want %q", got, want)
	}
	got = strings.Join(New().commandArgs("kill-session"), " ")
	if want := "kill-session"; got != want {
		t.Errorf("commandArgs on the default server = %q, want %q", got, want)
	}
}
