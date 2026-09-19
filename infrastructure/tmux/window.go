package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// SeatsSession is the tmux session a seat's window is opened in when mw is not
// itself running inside tmux: there is no session to add a window to, so the
// factory keeps one of its own, detached, and the person attaches to it when
// they want to see a seat.
const SeatsSession = "mw-seats"

// TmuxEnv is what tmux sets in every process it starts, and so how mw tells
// whether it is running inside tmux at all.
const TmuxEnv = "TMUX"

// psProgram is what a window's age is read from: tmux keeps no record of when
// a window was opened, but the process in its pane started with it, and how
// long it has been running is one column of ps.
const psProgram = "ps"

// Windows opens a seat's session in a window of one tmux session, and reports
// what is open there. It is the adapter behind application.Windows.
//
// Unlike a Runner's session, the window is not the factory's to watch: it
// holds a live seat that a person attaches to, types into and reads. Nothing
// here kills a window or sends anything to one.
type Windows struct {
	server
	// session is the tmux session windows are opened in, empty for the one mw
	// is itself running in (or SeatsSession, when it is running in none).
	session string
}

// Windows satisfies the port.
var _ application.Windows = (*Windows)(nil)

// NewWindows returns a Windows on tmux's default server, opening seats in the
// tmux session mw is itself running in — and, when mw is running outside tmux,
// in a detached session of the factory's own, SeatsSession. WithSession names
// one instead, and WithSocket says which tmux server to work on: tests must
// name a server of their own, so that no test ever opens a window among the
// ones a person is working in.
func NewWindows(opts ...Option) *Windows {
	s := chosen(opts)
	return &Windows{server: server{program: s.program, socket: s.socket}, session: s.session}
}

// Session reports the tmux session this Windows opens seats in, empty for
// whichever one mw is running in.
func (w *Windows) Session() string { return w.session }

// Open implements application.Windows: a new window of the seats' session,
// running the command with a terminal attached to it, left running when mw
// exits. The session itself is made, detached, when it is not there yet.
//
// The command is given to tmux as an argv, so no shell reads it: a kickoff
// prompt holding quotes, newlines or a dollar sign reaches the session exactly
// as it was written.
func (w *Windows) Open(ctx context.Context, spec application.WindowSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	// tmux splits its own arguments on ";", so a command holding one would be
	// cut in half and half of it run as a tmux command.
	for _, arg := range spec.Command {
		if arg == ";" {
			return fmt.Errorf("window %q: tmux reads a bare \";\" as the end of a command, so %v cannot be run as it is", spec.Name, spec.Command)
		}
	}

	session, err := w.sessionName(ctx)
	if err != nil {
		return err
	}
	there, err := w.sessionThere(ctx, session)
	if err != nil {
		return err
	}

	var args []string
	if there {
		args = []string{"new-window", "-d", "-t", target(session)}
	} else {
		args = []string{"new-session", "-d", "-s", session}
	}
	args = append(args, "-n", spec.Name)
	if spec.Dir != "" {
		args = append(args, "-c", spec.Dir)
	}
	for _, key := range sortedKeys(spec.Env) {
		args = append(args, "-e", key+"="+spec.Env[key])
	}
	_, err = w.call(ctx, append(args, spec.Command...)...)
	return err
}

// List implements application.Windows: every window of the seats' session,
// each with when it was opened.
//
// tmux keeps no record of when a window was opened — window_activity is the
// last time it printed anything, which for a live session is always now — so
// the window is dated by the process in its pane, which was started with it.
// A window whose process cannot be dated comes back with a zero time, which is
// what "not known" is: the caller must not read it as "opened just now".
func (w *Windows) List(ctx context.Context) ([]application.Window, error) {
	session, err := w.sessionName(ctx)
	if err != nil {
		return nil, err
	}
	there, err := w.sessionThere(ctx, session)
	if err != nil || !there {
		// A session that is not there holds no windows, which is not a failure:
		// nothing of this host's seats is running yet.
		return nil, err
	}

	printed, err := w.call(ctx, "list-windows", "-t", target(session), "-F", "#{window_name}|#{pane_pid}")
	if err != nil {
		return nil, err
	}

	var (
		open []application.Window
		pids []string
		at   = map[string]int{} // pid -> the window it is the pane of
	)
	for _, line := range strings.Split(string(printed), "\n") {
		name, pid, split := strings.Cut(strings.TrimSpace(line), "|")
		if !split || name == "" {
			continue
		}
		open = append(open, application.Window{Name: name})
		if pid != "" {
			pids = append(pids, pid)
			at[pid] = len(open) - 1
		}
	}

	for pid, started := range startTimes(ctx, pids) {
		open[at[pid]].Opened = started
	}
	return open, nil
}

// sessionName is the tmux session a seat's window belongs in: the one named by
// WithSession, else the one mw is itself running in, else the factory's own.
func (w *Windows) sessionName(ctx context.Context) (string, error) {
	if w.session != "" {
		return w.session, nil
	}
	if os.Getenv(TmuxEnv) == "" {
		return SeatsSession, nil
	}
	// display-message with no target names the session of the client mw is
	// running in, which is the one the person is looking at.
	printed, err := w.call(ctx, "display-message", "-p", "#S")
	if err != nil {
		return "", fmt.Errorf("asking tmux which session this is: %w", err)
	}
	session := strings.TrimSpace(string(printed))
	if session == "" {
		return SeatsSession, nil
	}
	return session, nil
}

// sessionThere reports whether the server has that session. tmux saying no
// with a non-zero status — no such session, or no server running at all — is
// an answer, not a failure.
func (w *Windows) sessionThere(ctx context.Context, session string) (bool, error) {
	err := exec.CommandContext(ctx, w.program, w.commandArgs("has-session", "-t", sessionTarget(session))...).Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return false, err
}

// startTimes is when each process was started, by pid, read in one ps: its
// elapsed seconds subtracted from now. A pid ps says nothing about is left
// out, and a ps that fails leaves them all out — a window that cannot be dated
// is reported as not dated rather than as an error, because being unable to
// say how old a window is must not stop mw reporting that it is open.
func startTimes(ctx context.Context, pids []string) map[string]time.Time {
	started := map[string]time.Time{}
	if len(pids) == 0 {
		return started
	}
	printed, err := exec.CommandContext(ctx, psProgram, "-o", "pid=,etimes=", "-p", strings.Join(pids, ",")).Output()
	if err != nil {
		return started
	}

	now := time.Now()
	for _, line := range strings.Split(string(printed), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		seconds, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		started[fields[0]] = now.Add(-time.Duration(seconds) * time.Second)
	}
	return started
}
