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

// Runner starts and watches sessions on one tmux server.
type Runner struct {
	socket string
	poll   time.Duration
}

// Runner satisfies the port.
var _ application.Runner = (*Runner)(nil)

// Option is a setting of a Runner, given to New.
type Option func(*Runner)

// WithSocket names the tmux server to work on: tmux's -L. Empty, the default,
// is tmux's default server — the one holding whatever a person has open on this
// machine. Tests must set it to a name of their own and kill that server when
// they are done.
func WithSocket(name string) Option {
	return func(r *Runner) { r.socket = name }
}

// WithPollInterval sets how long Wait first sleeps between looks at a session.
// It backs off from there up to a couple of seconds.
func WithPollInterval(d time.Duration) Option {
	return func(r *Runner) { r.poll = d }
}

// New returns a Runner on tmux's default server, unless an option says
// otherwise.
func New(opts ...Option) *Runner {
	r := &Runner{poll: defaultPollInterval}
	for _, opt := range opts {
		opt(r)
	}
	if r.poll <= 0 {
		r.poll = defaultPollInterval
	}
	return r
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
func (r *Runner) Status(ctx context.Context, name string) (application.SessionStatus, error) {
	out, err := r.call(ctx, "list-panes", "-t", target(name), "-F", "#{pane_dead}|#{pane_dead_status}")
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
	err := exec.CommandContext(ctx, Program, r.commandArgs("has-session", "-t", sessionTarget(name))...).Run()
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
func (r *Runner) commandArgs(args ...string) []string {
	if r.socket == "" {
		return args
	}
	return append([]string{"-L", r.socket}, args...)
}

// target names a session's current window, exactly. The leading "=" is tmux's
// "this name and no other": without it tmux would also take the name as a
// pattern and could act on a session the factory did not mean.
func target(name string) string { return "=" + name + ":" }

// sessionTarget names a session itself, exactly.
func sessionTarget(name string) string { return "=" + name }

// parseStatus reads what tmux printed for a session's panes. A window can hold
// more than one pane; the session is running while any of them is, and the exit
// status is the first one that finished left behind.
func parseStatus(name string, printed []byte) (application.SessionStatus, error) {
	var (
		panes    int
		running  bool
		exitCode int
		known    bool
	)
	for _, line := range strings.Split(string(printed), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		panes++
		dead, status, found := strings.Cut(line, "|")
		if !found {
			return application.SessionStatus{}, fmt.Errorf("reading the state of session %q: tmux printed %q", name, line)
		}
		switch dead {
		case "0":
			running = true
		case "1":
			// A dead pane tmux has no status for — killed rather than exited —
			// counts as having exited with nothing to report.
			if status == "" || known {
				continue
			}
			code, err := strconv.Atoi(status)
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
	}
	return application.SessionStatus{Name: name, State: application.StateExited, ExitCode: exitCode}, nil
}

// call runs one tmux command and returns its standard output.
func (r *Runner) call(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, Program, r.commandArgs(args...)...)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if said := strings.TrimSpace(errs.String()); said != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", Program, strings.Join(args, " "), err, said)
		}
		return nil, fmt.Errorf("%s %s: %w", Program, strings.Join(args, " "), err)
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
