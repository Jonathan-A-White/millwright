package rig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// AfterLanding runs the command a host names for a rig once a landing has moved
// its checkout of it. It is the adapter behind application.AfterLanding, and it
// reads the command line the way Checks reads a rig's tests: with the shell, in
// the rig's own directory, so that a host says what it means in its own words.
type AfterLanding struct {
	commands map[string]string
	limit    time.Duration
	limits   map[string]time.Duration
	shell    string
}

// AfterLanding satisfies the port.
var _ application.AfterLanding = (*AfterLanding)(nil)

// AfterLandingOption is a setting of an AfterLanding, given to NewAfterLanding.
type AfterLandingOption func(*AfterLanding)

// WithAfterCommands names the command line each rig has, by rig name, as the
// [after_landing] table of the config file has it. A rig the table does not name
// has none, and nothing is run for it.
func WithAfterCommands(commands map[string]string) AfterLandingOption {
	return func(a *AfterLanding) {
		for rig, command := range commands {
			if strings.TrimSpace(command) != "" {
				a.commands[rig] = command
			}
		}
	}
}

// WithAfterLimits names the limit each rig has, by rig name, as the
// [after_landing_limit] table of the config file has it: a rig that deploys over
// a slow uplink is given longer than the default. A rig the table does not name,
// or names with no time, keeps the default.
func WithAfterLimits(limits map[string]time.Duration) AfterLandingOption {
	return func(a *AfterLanding) {
		for rig, limit := range limits {
			if limit > 0 {
				a.limits[rig] = limit
			}
		}
	}
}

// WithAfterLimit is how long a command may run before it is stopped, instead of
// application.AfterLandingLimit, for every rig whose own limit is not named. It
// is how a test makes a command that outlives its limit without waiting minutes
// for it.
func WithAfterLimit(limit time.Duration) AfterLandingOption {
	return func(a *AfterLanding) {
		if limit > 0 {
			a.limit = limit
		}
	}
}

// WithAfterShell names the shell that reads the command line.
func WithAfterShell(path string) AfterLandingOption {
	return func(a *AfterLanding) { a.shell = path }
}

// NewAfterLanding returns an AfterLanding that runs nothing until it is told
// which rigs have a command.
func NewAfterLanding(opts ...AfterLandingOption) *AfterLanding {
	a := &AfterLanding{commands: map[string]string{}, limits: map[string]time.Duration{}, limit: application.AfterLandingLimit, shell: Shell}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Command implements application.AfterLanding.
func (a *AfterLanding) Command(rig string) string { return a.commands[rig] }

// Limit is how long the rig's command may run: the limit the host names for it,
// else the default.
func (a *AfterLanding) Limit(rig string) time.Duration {
	if limit, ok := a.limits[rig]; ok {
		return limit
	}
	return a.limit
}

// Run implements application.AfterLanding.
func (a *AfterLanding) Run(ctx context.Context, rig, dir string) (application.Ran, error) {
	command := a.Command(rig)
	if command == "" {
		return application.Ran{}, fmt.Errorf("the rig %s has no command to run after a landing", rig)
	}
	if dir == "" {
		return application.Ran{}, fmt.Errorf("running `%s`: where?", command)
	}
	if _, err := os.Stat(dir); err != nil {
		return application.Ran{}, fmt.Errorf("running `%s`: %w", command, err)
	}

	steps := commandSteps(command)
	line := command
	if len(steps) > 1 {
		line = markSteps(steps)
	}
	limit := a.Limit(rig)
	limited, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	output, err := runLine(limited, a.shell, line, dir, 0, freePortEnv())
	ran := application.Ran{Command: command, Output: output, Steps: steps, Step: stepEndedIn(output, steps)}
	if err == nil {
		return ran, nil
	}

	// The limit is what ended it, and the caller's own context is not: a
	// command stopped because mw itself was told to stop is not one that took
	// too long.
	if limited.Err() != nil && ctx.Err() == nil {
		ran.Status, ran.TimedOut = -1, limit
		return ran, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && ctx.Err() == nil {
		ran.Status = exited.ExitCode()
		return ran, nil
	}
	return application.Ran{}, fmt.Errorf("running `%s` in %s: %w: %s", command, dir, err, application.RecentLines(output, 10))
}

// PreviewPortEnv is the variable a rig's after-landing command finds a free
// port of its own in, picked afresh for every run: postern's shots start their
// preview server on it (tests/support/preview-port.ts), and two runs on one
// host never ask for the same port, as two took 5370 when it was named by the
// checkout's path (mw-gq6.479). The kernel picks it from its ephemeral range,
// clear of the 4400-5399 postern's gate runs derive from their worktrees.
const PreviewPortEnv = "PREVIEW_PORT"

// freePortEnv names a port nothing listens on now, as the one variable of the
// command's environment; nil when none could be had, and then the command
// picks its own as it always did.
func freePortEnv() []string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil
	}
	defer listener.Close()
	return []string{fmt.Sprintf("%s=%d", PreviewPortEnv, listener.Addr().(*net.TCPAddr).Port)}
}

// markSteps is the command line that runs steps as the &&s between them do,
// in the one shell, with each step's mark printed before it.
func markSteps(steps []string) string {
	marked := make([]string, 0, 2*len(steps))
	for i, step := range steps {
		marked = append(marked, "printf '%s\\n' "+shellQuote(application.AfterLandingStepMark(i+1, len(steps), step)), step)
	}
	return strings.Join(marked, " && ")
}

// stepEndedIn is the step, counted from 1, whose mark the output holds last:
// the one that was running when the command ended. A command of one step
// ended in it.
func stepEndedIn(output string, steps []string) int {
	if len(steps) == 1 {
		return 1
	}
	for i := len(steps); i >= 1; i-- {
		if strings.Contains(output, application.AfterLandingStepMark(i, len(steps), steps[i-1])+"\n") {
			return i
		}
	}
	return 0
}

// shellQuote is s as one word of the shell, in single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// commandSteps is a command line read as the steps its top-level &&s join,
// each trimmed: the &&s inside quotes, backquotes, parentheses, braces or $( )
// are a step's own. A line that is anything but a plain chain of &&s — one that
// holds a ;, a ||, a lone &, a newline, a comment, an empty step, or a quote or
// bracket left open — is one step, the line as it is.
func commandSteps(command string) []string {
	whole := []string{command}
	var steps []string
	depth, start := 0, 0
	var quote rune
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			}
			continue
		case r == '\\':
			i++
			continue
		case quote == '"' || quote == '`':
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
		case '(', '{':
			depth++
		case ')', '}':
			depth--
			if depth < 0 {
				return whole
			}
		}
		if depth > 0 {
			continue
		}
		switch {
		case r == ';' || r == '\n' || r == '#' && (i == 0 || runes[i-1] == ' ' || runes[i-1] == '\t'):
			return whole
		case r == '|' && i+1 < len(runes) && runes[i+1] == '|':
			return whole
		case r == '&' && i+1 < len(runes) && runes[i+1] == '&':
			step := strings.TrimSpace(string(runes[start:i]))
			if step == "" {
				return whole
			}
			steps = append(steps, step)
			start = i + 2
			i++
		case r == '&' && (i > 0 && (runes[i-1] == '>' || runes[i-1] == '<') || i+1 < len(runes) && runes[i+1] == '>'):
			// A redirection: 2>&1, >&2, &>file.
		case r == '&':
			return whole
		}
	}
	last := strings.TrimSpace(string(runes[start:]))
	if quote != 0 || depth != 0 || last == "" {
		return whole
	}
	return append(steps, last)
}
