package rig

import (
	"context"
	"errors"
	"fmt"
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

// WithAfterLimit is how long a command may run before it is stopped, instead of
// application.AfterLandingLimit. It is how a test makes a command that outlives
// its limit without waiting minutes for it.
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
	a := &AfterLanding{commands: map[string]string{}, limit: application.AfterLandingLimit, shell: Shell}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Command implements application.AfterLanding.
func (a *AfterLanding) Command(rig string) string { return a.commands[rig] }

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

	limited, cancel := context.WithTimeout(ctx, a.limit)
	defer cancel()
	output, err := runLine(limited, a.shell, command, dir)
	ran := application.Ran{Command: command, Output: output}
	if err == nil {
		return ran, nil
	}

	// The limit is what ended it, and the caller's own context is not: a
	// command stopped because mw itself was told to stop is not one that took
	// too long.
	if limited.Err() != nil && ctx.Err() == nil {
		ran.Status, ran.TimedOut = -1, a.limit
		return ran, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && ctx.Err() == nil {
		ran.Status = exited.ExitCode()
		return ran, nil
	}
	return application.Ran{}, fmt.Errorf("running `%s` in %s: %w: %s", command, dir, err, application.RecentLines(output, 10))
}
