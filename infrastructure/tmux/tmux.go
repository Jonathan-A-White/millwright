// Package tmux runs the factory's sessions by shelling out to tmux: one tmux
// session per story, named after it, so that a person can attach to any story
// the factory is working. It is the adapter behind application.Runner.
//
// A tmux server is shared with whoever else is using it, so this package is
// careful with it: it changes no global option, it names one session exactly
// rather than letting tmux match a pattern, and it touches no session it was
// not asked about. Which server it works on is WithSocket — tmux's -L. The
// default, the empty socket, is tmux's own default server, which is where the
// factory's sessions belong; tests must name a server of their own instead, so
// that they never reach into the sessions a person is working in.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Program is the command this package shells out to.
const Program = "tmux"

// How often Wait asks tmux whether a session's command is still running: often
// at first, so that a short command is noticed at once, then more slowly, so
// that a session running for an hour costs almost nothing to watch.
const (
	defaultPollInterval = 50 * time.Millisecond
	maxPollInterval     = 2 * time.Second
)

// How long a pane that is dead with no exit status is given to be reaped before
// Status stops waiting for the status and reports it unknown. The gap is
// ordinarily a few milliseconds; a box loaded enough to stretch it past this is
// rarer than the case this exists for, a pane tmux reaped without ever noting
// how it ended, whose status never comes.
const defaultReapGrace = 5 * time.Second

// The longest Status sleeps between looks at a pane that is dead with no status.
const maxReapPoll = 100 * time.Millisecond

// server is one tmux server, and how this package talks to it: which tmux
// command to run and which server it is. A Runner and a Windows are both one
// of these, so the two are always looking at the same tmux.
type server struct {
	program string
	socket  string
}

// Runner starts and watches sessions on one tmux server.
type Runner struct {
	server
	poll  time.Duration
	grace time.Duration
}

// Runner satisfies the port.
var _ application.Runner = (*Runner)(nil)

// settings are what a Runner or a Windows is built from. One Option sets any
// of them, so that both are pointed at a server, and at a tmux, the same way.
type settings struct {
	program string
	socket  string
	poll    time.Duration
	grace   time.Duration
	session string
}

// Option is a setting of a Runner or a Windows, given to New or NewWindows.
type Option func(*settings)

// WithSocket names the tmux server to work on: tmux's -L. Empty, the default,
// is tmux's default server — the one holding whatever a person has open on this
// machine. Tests must set it to a name of their own and kill that server when
// they are done.
func WithSocket(name string) Option {
	return func(s *settings) { s.socket = name }
}

// WithPollInterval sets how long Wait first sleeps between looks at a session.
// It backs off from there up to a couple of seconds.
func WithPollInterval(d time.Duration) Option {
	return func(s *settings) { s.poll = d }
}

// WithProgram names the tmux command to run, for a host that keeps it somewhere
// unusual — and for a test that needs a stand-in for tmux rather than the real
// thing.
func WithProgram(program string) Option {
	return func(s *settings) { s.program = program }
}

// WithReapGrace sets how long Status gives a pane that is dead with no exit
// status to be reaped before it reports the status unknown.
func WithReapGrace(d time.Duration) Option {
	return func(s *settings) { s.grace = d }
}

// WithSession names the tmux session a Windows opens a seat's window in,
// instead of the one mw is itself running in. See NewWindows.
func WithSession(name string) Option {
	return func(s *settings) { s.session = name }
}

// chosen is the settings the options add up to, over this package's defaults.
func chosen(opts []Option) settings {
	s := settings{program: Program, poll: defaultPollInterval, grace: defaultReapGrace}
	for _, opt := range opts {
		opt(&s)
	}
	if s.poll <= 0 {
		s.poll = defaultPollInterval
	}
	if s.grace < 0 {
		s.grace = defaultReapGrace
	}
	return s
}

// New returns a Runner on tmux's default server, unless an option says
// otherwise.
func New(opts ...Option) *Runner {
	s := chosen(opts)
	return &Runner{server: server{program: s.program, socket: s.socket}, poll: s.poll, grace: s.grace}
}

// Socket reports the tmux server this Runner works on, empty for the default.
func (r *Runner) Socket() string { return r.socket }

// Available reports whether tmux is on PATH. Tests that need a real tmux skip
// themselves when it is not.
func Available() bool {
	_, err := exec.LookPath(Program)
	return err == nil
}

// Start implements application.Runner.
func (r *Runner) Start(ctx context.Context, spec application.SessionSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	// tmux splits its own arguments on ";", so a command holding one would be
	// cut in half and half of it run as a tmux command.
	for _, arg := range spec.Command {
		if arg == ";" {
			return fmt.Errorf("session %q: tmux reads a bare \";\" as the end of a command, so %v cannot be run as it is", spec.Name, spec.Command)
		}
	}
	_, err := r.call(ctx, startArgs(spec)...)
	return err
}

// Send implements application.Runner. The text is typed literally and each
// newline is the Enter key, so that a command reading a line gets one.
func (r *Runner) Send(ctx context.Context, name, input string) error {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		if line != "" {
			if _, err := r.call(ctx, "send-keys", "-t", target(name), "-l", "--", line); err != nil {
				return err
			}
		}
		if i < len(lines)-1 {
			if _, err := r.call(ctx, "send-keys", "-t", target(name), "Enter"); err != nil {
				return err
			}
		}
	}
	return nil
}

// Output implements application.Runner. It reads the pane's history as well as
// its screen: a command that has exited leaves its output in the history, and
// the screen it leaves behind is blank.
func (r *Runner) Output(ctx context.Context, name string, lines int) (string, error) {
	if lines < 1 {
		return "", fmt.Errorf("reading a session needs at least 1 line, got %d", lines)
	}
	out, err := r.call(ctx, "capture-pane", "-p", "-S", "-"+strconv.Itoa(lines), "-E", "-", "-t", target(name))
	if err != nil {
		return "", err
	}
	return application.RecentLines(string(out), lines), nil
}

// Status implements application.Runner. remain-on-exit, which Start sets on the
// session's window, is what leaves a finished command's exit status to be read:
// tmux keeps the dead pane and reports what it exited with.
//
// A pane is marked dead as soon as its terminal closes, which can be a moment
// before tmux has reaped the command and knows how it ended. Status looks again
// through that gap rather than call the command a clean exit. The gap is not
// always a passing one: with one CPU to run on tmux is often seen to reap a
// command without noting how it ended, and then no status ever arrives. So the
// gap is given a grace, and a pane still dead with no status when it is up is
// reported as StateExitUnknown, which is not a finished command and not a clean
// exit either.
func (r *Runner) Status(ctx context.Context, name string) (application.SessionStatus, error) {
	var gapSince time.Time
	sleep := time.Millisecond
	for {
		status, err := r.look(ctx, name)
		if err != nil || status.State != stateUnreaped {
			return status, err
		}

		if gapSince.IsZero() {
			gapSince = time.Now()
		}
		left := r.grace - time.Since(gapSince)
		if left <= 0 {
			return application.SessionStatus{Name: name, State: application.StateExitUnknown}, nil
		}
		timer := time.NewTimer(min(sleep, left, maxReapPoll))
		select {
		case <-ctx.Done():
			timer.Stop()
			return application.SessionStatus{}, ctx.Err()
		case <-timer.C:
		}
		sleep *= 2
	}
}

// look asks tmux once about a session's panes.
func (r *Runner) look(ctx context.Context, name string) (application.SessionStatus, error) {
	out, err := r.call(ctx, "list-panes", "-t", target(name), "-F", "#{pane_dead}|#{pane_dead_status}|#{pane_dead_signal}")
	if err != nil {
		if there, checked := r.exists(ctx, name); checked == nil && !there {
			return application.SessionStatus{Name: name, State: application.StateGone}, nil
		}
		return application.SessionStatus{}, err
	}
	return parseStatus(name, out)
}

// Wait implements application.Runner.
func (r *Runner) Wait(ctx context.Context, name string) (application.SessionStatus, error) {
	sleep := r.poll
	var last application.SessionStatus
	for {
		status, err := r.Status(ctx, name)
		if err != nil {
			// A context that ran out while tmux was being asked is the caller
			// giving up, not tmux failing.
			if ctx.Err() != nil {
				return last, ctx.Err()
			}
			return application.SessionStatus{}, err
		}
		if !status.Running() {
			return status, nil
		}
		last = status

		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return status, ctx.Err()
		case <-timer.C:
		}
		sleep *= 2
		if sleep > maxPollInterval {
			sleep = maxPollInterval
		}
	}
}

// Close implements application.Runner. Killing a session that is already gone
// is not a failure: the session is gone either way.
func (r *Runner) Close(ctx context.Context, name string) error {
	if _, err := r.call(ctx, "kill-session", "-t", sessionTarget(name)); err != nil {
		if there, checked := r.exists(ctx, name); checked == nil && !there {
			return nil
		}
		return err
	}
	return nil
}

// exists reports whether the server has a session of that name. The second
// return is the reason it could not be asked — tmux missing, say — which is not
// the same as an answer of no.
func (r *Runner) exists(ctx context.Context, name string) (bool, error) {
	err := exec.CommandContext(ctx, r.program, r.commandArgs("has-session", "-t", sessionTarget(name))...).Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// tmux says no with a non-zero status: no such session, or no server
		// running at all, which amounts to the same thing.
		return false, nil
	}
	return false, err
}

// startArgs is the tmux command line that starts one session: a detached
// session running the command, and remain-on-exit set on that session's window
// so that the command's exit status and last output survive it.
//
// Both go in one tmux invocation, because a command that exits at once would
// otherwise be reaped before a second invocation could set the option. The
// option is set on this window only: never globally, because the server is
// shared with whoever else is using this machine.
func startArgs(spec application.SessionSpec) []string {
	args := []string{"new-session", "-d", "-s", spec.Name}
	if spec.Dir != "" {
		args = append(args, "-c", spec.Dir)
	}
	for _, key := range sortedKeys(spec.Env) {
		args = append(args, "-e", key+"="+spec.Env[key])
	}
	args = append(args, spec.Command...)
	return append(args, ";", "set-option", "-t", target(spec.Name), "remain-on-exit", "on")
}

// commandArgs is what tmux is run with: the server to talk to, then the
// command.
func (s server) commandArgs(args ...string) []string {
	if s.socket == "" {
		return args
	}
	return append([]string{"-L", s.socket}, args...)
}

// target names a session's current window, exactly. The leading "=" is tmux's
// "this name and no other": without it tmux would also take the name as a
// pattern and could act on a session the factory did not mean.
func target(name string) string { return "=" + name + ":" }

// sessionTarget names a session itself, exactly.
func sessionTarget(name string) string { return "=" + name }

// stateUnreaped is what parseStatus says of a session whose command looks to
// have ended but whose exit status tmux has not given yet. It is not one of the
// states a Runner reports: Status looks again, and after its grace reports the
// exit unknown.
const stateUnreaped application.SessionState = "unreaped"

// parseStatus reads what tmux printed for a session's panes. A window can hold
// more than one pane; the session is running while any of them is, and the exit
// status is the first one that finished left behind.
//
// A dead pane with neither an exit status nor a signal is one tmux has not
// finished reaping: it drops the pane's terminal, and so marks it dead, before
// it has collected the command's status. It is neither running nor exited 0 —
// a command that exited 3 must not be read as having exited 0 — but stateUnreaped,
// to be looked at again. A live pane in the window still makes the session
// running.
func parseStatus(name string, printed []byte) (application.SessionStatus, error) {
	var (
		panes    int
		running  bool
		unreaped bool
		exitCode int
		known    bool
	)
	for _, line := range strings.Split(string(printed), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		panes++
		fields := strings.Split(line, "|")
		if len(fields) != 3 {
			return application.SessionStatus{}, fmt.Errorf("reading the state of session %q: tmux printed %q", name, line)
		}
		dead, status, signal := fields[0], fields[1], fields[2]
		switch dead {
		case "0":
			running = true
		case "1":
			if status == "" && signal == "" {
				unreaped = true
				continue
			}
			if known {
				continue
			}
			// A dead pane tmux has a signal but no status for was killed rather
			// than exited. It reports as a shell does, 128 and the signal, so that
			// it is never taken for a clean exit.
			code, err := strconv.Atoi(status)
			if status == "" {
				var sig int
				if sig, err = strconv.Atoi(signal); err == nil {
					code = 128 + sig
				}
			}
			if err != nil {
				return application.SessionStatus{}, fmt.Errorf("reading the exit status of session %q: tmux printed %q", name, line)
			}
			exitCode, known = code, true
		default:
			return application.SessionStatus{}, fmt.Errorf("reading the state of session %q: tmux printed %q", name, line)
		}
	}
	switch {
	case panes == 0:
		return application.SessionStatus{}, fmt.Errorf("reading the state of session %q: tmux printed nothing", name)
	case running:
		return application.SessionStatus{Name: name, State: application.StateRunning}, nil
	case unreaped:
		return application.SessionStatus{Name: name, State: stateUnreaped}, nil
	}
	return application.SessionStatus{Name: name, State: application.StateExited, ExitCode: exitCode}, nil
}

// call runs one tmux command and returns its standard output.
func (s server) call(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, s.program, s.commandArgs(args...)...)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if said := strings.TrimSpace(errs.String()); said != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", s.program, strings.Join(args, " "), err, said)
		}
		return nil, fmt.Errorf("%s %s: %w", s.program, strings.Join(args, " "), err)
	}
	return out.Bytes(), nil
}

// sortedKeys is a map's keys in order, so that a tmux command line is the same
// every time it is built from the same session.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
