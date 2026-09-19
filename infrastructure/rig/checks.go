package rig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultCommand is what a rig's tests are when the factory has not been told
// otherwise, and Shell is what reads the command line. A line rather than an
// argv, because a rig says what its tests are in its own words — `make test`,
// `go test ./...`, `npm test && npm run lint` — and a shell is what reads those.
const (
	DefaultCommand = "make test"
	Shell          = "/bin/sh"
)

// The exit statuses a POSIX shell gives a command it could not run at all.
const (
	exitNotExecutable = 126
	exitNotFound      = 127
)

// Checks runs one rig's own tests. It is the adapter behind application.Checks.
//
// What the tests are is per rig and per host: the factory does not know how any
// rig is built, only that the rig can say whether it is green. Tests that fail
// are an answer, not an error — the error is a command that could not be run at
// all, which is a host that is set up wrong rather than a story that is broken.
type Checks struct {
	commands map[string]string
	command  string
	shell    string
}

// Checks satisfies the port.
var _ application.Checks = (*Checks)(nil)

// CheckOption is a setting of a Checks, given to NewChecks.
type CheckOption func(*Checks)

// WithCommand names the command line every rig's tests are, instead of
// DefaultCommand. It is how a test points the factory at something trivial: a
// test of mw that ran mw's own test suite inside itself would never finish.
func WithCommand(command string) CheckOption {
	return func(c *Checks) {
		if strings.TrimSpace(command) != "" {
			c.command = command
		}
	}
}

// WithCommands names the command line each rig's tests are, by rig name, as the
// [tests] table of the config file has it. A rig the table does not name is
// tested with the command WithCommand set, or DefaultCommand.
func WithCommands(commands map[string]string) CheckOption {
	return func(c *Checks) {
		for rig, command := range commands {
			if strings.TrimSpace(command) != "" {
				c.commands[rig] = command
			}
		}
	}
}

// WithCheckShell names the shell that reads the command line.
func WithCheckShell(path string) CheckOption {
	return func(c *Checks) { c.shell = path }
}

// NewChecks returns the checks of a rig whose tests are DefaultCommand, unless
// an option says otherwise.
func NewChecks(opts ...CheckOption) *Checks {
	c := &Checks{commands: map[string]string{}, command: DefaultCommand, shell: Shell}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Command reports the command line a rig's tests are run by.
func (c *Checks) Command(rig string) string {
	if command, named := c.commands[rig]; named {
		return command
	}
	return c.command
}

// Run implements application.Checks.
func (c *Checks) Run(ctx context.Context, rig, dir string) (application.Checked, error) {
	command := c.Command(rig)
	if dir == "" {
		return application.Checked{}, fmt.Errorf("running `%s`: where?", command)
	}
	if _, err := os.Stat(dir); err != nil {
		return application.Checked{}, fmt.Errorf("running `%s`: %w", command, err)
	}

	cmd := exec.CommandContext(ctx, c.shell, "-c", command)
	cmd.Dir = dir
	// Nobody is at the keyboard: a test that asks git for a password would wait
	// there forever in a pane nobody is watching.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	checked := application.Checked{Command: command, Output: out.String(), Passed: err == nil}
	if err == nil {
		return checked, nil
	}

	// A command that ran and failed is the rig saying no. Anything else — no
	// shell, no such directory, a context that gave up — is this host being
	// unable to ask the question at all.
	var exited *exec.ExitError
	if errors.As(err, &exited) && ctx.Err() == nil {
		// The shell's own words for a command it could not run: not found, or found
		// and not executable. That is a toolchain this host does not have on its
		// PATH, not a test that is red, and a person reading "the tests fail" would
		// go looking at the code.
		code := exited.ExitCode()
		checked.NotRun = code == exitNotExecutable || code == exitNotFound
		return checked, nil
	}
	return application.Checked{}, fmt.Errorf("running `%s` in %s: %w: %s", command, dir, err, application.RecentLines(out.String(), 10))
}
