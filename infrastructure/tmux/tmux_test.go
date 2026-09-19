package tmux

// The unit tests here cover the parts that are ours rather than tmux's: the
// arguments a session is started with, the targets tmux is pointed at, and the
// reading of what it prints back. tmux itself is exercised by the integration
// test, which runs against a private tmux server.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

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
		{"a live pane", "0||\n", application.StateRunning, 0},
		{"a command that exited well", "1|0|\n", application.StateExited, 0},
		{"a command that failed", "1|7|\n", application.StateExited, 7},
		{"a command tmux has not finished reaping", "1||\n", stateUnreaped, 0},
		{"a command that was killed", "1||9\n", application.StateExited, 137},
		{"a window with a second pane in it", "0||\n0||\n", application.StateRunning, 0},
		{"a second pane still being reaped", "1|3|\n1||\n", stateUnreaped, 0},
		{"a live pane beside one still being reaped", "0||\n1||\n", application.StateRunning, 0},
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
	for _, printed := range []string{"", "\n", "yes|0|\n", "1|later|\n", "1||nine\n", "1|0\n"} {
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

// standInTmux writes a program that answers every call with the next of the
// lines it is given — the last one again and again once they run out — and
// returns a Runner that runs it instead of tmux. A pane tmux never learns the
// status of cannot be had from a real tmux on demand: it is a race that only
// some loaded boxes lose.
func standInTmux(t *testing.T, printed ...string) *Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for tmux is a shell script")
	}
	dir := t.TempDir()
	var script strings.Builder
	fmt.Fprintf(&script, "#!/bin/sh\ncalls=%q\necho x >> \"$calls\"\nn=$(wc -l < \"$calls\")\ncase $n in\n", filepath.Join(dir, "calls"))
	for i, line := range printed {
		pattern := "*"
		if i < len(printed)-1 {
			pattern = strconv.Itoa(i + 1)
		}
		fmt.Fprintf(&script, "  %s) echo '%s' ;;\n", pattern, line)
	}
	script.WriteString("esac\n")
	path := filepath.Join(dir, "tmux-stand-in")
	if err := os.WriteFile(path, []byte(script.String()), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return New(WithProgram(path), WithPollInterval(5*time.Millisecond), WithReapGrace(150*time.Millisecond))
}

func TestWaitGivesUpOnAPaneThatStaysDeadWithNoStatusAndSaysItsExitIsUnknown(t *testing.T) {
	runner := standInTmux(t, "1||")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	started := time.Now()
	status, err := runner.Wait(ctx, "mw-gq6_4")
	if err != nil {
		t.Fatalf("expected Wait to return the status it could make of the pane, got %v after %s", err, time.Since(started))
	}
	if status.State != application.StateExitUnknown {
		t.Errorf("expected the exit to be reported unknown, got %+v", status)
	}
	if status.Running() || status.Finished() {
		t.Errorf("expected a pane that ended with no status to be neither running nor finished, got %+v", status)
	}
	if took := time.Since(started); took < 100*time.Millisecond {
		t.Errorf("expected Wait to give the pane its grace to be reaped first, returned after %s", took)
	}
}

func TestWaitStillReadsTheStatusOfAPaneReapedAfterAGap(t *testing.T) {
	// The original flake: dead, then a moment later dead with status 3. The
	// grace is there for this, and the 3 must come back as 3.
	runner := standInTmux(t, "1||", "1||", "1|3|")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	status, err := runner.Wait(ctx, "mw-gq6_4")
	if err != nil {
		t.Fatalf("expected Wait to return, got %v", err)
	}
	if !status.Finished() || status.ExitCode != 3 {
		t.Errorf("expected the command to have exited 3, got %+v", status)
	}
}

func TestStatusStopsLookingWhenTheCallerDoes(t *testing.T) {
	runner := standInTmux(t, "1||")
	runner.grace = time.Minute
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, err := runner.Status(ctx, "mw-gq6_4"); err == nil {
		t.Error("expected a Status cut short by its context to say so")
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("expected Status to return when its context ran out, took %s", took)
	}
}
