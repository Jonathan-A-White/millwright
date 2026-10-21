package rig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

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
	parts    map[string][]application.GatePart
	specs    map[string]string
	related  map[string]string
	named    map[string]string
	timing   map[string][]string
	git      string
	shell    string
	nice     int
	jobs     int
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

// WithParts names the parts each rig's gate is split into, by rig name, as the
// `[test_parts.<rig>]` tables of the config file have them (mw-gq6.567). A rig
// the map does not name has its tests run whole.
func WithParts(parts map[string][]application.GatePart) CheckOption {
	return func(c *Checks) {
		for rig, named := range parts {
			if len(named) > 0 {
				c.parts[rig] = named
			}
		}
	}
}

// WithCheckShell names the shell that reads the command line.
func WithCheckShell(path string) CheckOption {
	return func(c *Checks) { c.shell = path }
}

// WithNice runs every rig's tests under `nice -n <n>`, so they yield the CPU to
// the mill's own work on the same host (mw-gq6.312). 0, the default, runs them
// as they were.
func WithNice(n int) CheckOption {
	return func(c *Checks) { c.nice = n }
}

// WithJobs caps the test workers every rig's tests may start at n, by handing
// the command JOBS, GOMAXPROCS and VITEST_MAX_WORKERS (application.WorkerLimitEnv),
// so a host that is also someone's PC keeps cores free (mw-gq6.399). 0, the
// default, leaves the command the environment it was started in.
func WithJobs(n int) CheckOption {
	return func(c *Checks) { c.jobs = n }
}

// WithTimingTests gives each rig's commands MW_TIMING=skip and MW_TIMING_TESTS
// as the rig's timing_tests globs, by rig name (application.TimingEnv), so the
// tests that need a real clock leave the gate and the suite runs. A rig the map
// does not name, or names with no globs, is run as it was.
func WithTimingTests(timing map[string][]string) CheckOption {
	return func(c *Checks) {
		for rig, globs := range timing {
			if len(globs) > 0 {
				c.timing[rig] = globs
			}
		}
	}
}

// NewChecks returns the checks of a rig whose tests are DefaultCommand, unless
// an option says otherwise.
func NewChecks(opts ...CheckOption) *Checks {
	c := &Checks{commands: map[string]string{}, command: DefaultCommand, parts: map[string][]application.GatePart{}, specs: map[string]string{}, related: map[string]string{}, named: map[string]string{}, timing: map[string][]string{}, git: Program, shell: Shell}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// commandEnv is the worker limit and the rig's timing tests as KEY=VALUE words
// for the environment of a command run for the rig.
func (c *Checks) commandEnv(rig string) []string {
	var env []string
	for name, value := range application.WorkerLimitEnv(c.jobs) {
		env = append(env, name+"="+value)
	}
	for name, value := range application.TimingEnv(application.TimingSkip, c.timing[rig]) {
		env = append(env, name+"="+value)
	}
	return env
}

// Command reports the command line a rig's tests are run by.
func (c *Checks) Command(rig string) string {
	if command, named := c.commands[rig]; named {
		return command
	}
	return c.command
}

// Parts implements application.GateParts: the parts a rig's gate is named as, in
// the order they are run; none when its tests run whole.
func (c *Checks) Parts(rig string) []application.GatePart { return c.parts[rig] }

// Checks satisfies the port a split gate is read and run through.
var _ application.GateParts = (*Checks)(nil)

// Run implements application.Checks.
func (c *Checks) Run(ctx context.Context, rig, dir string) (application.Checked, error) {
	return c.RunLine(ctx, rig, dir, c.Command(rig))
}

// Checks satisfies the port a suite's command lines are run through.
var _ application.SuiteRunner = (*Checks)(nil)

// RunLine implements application.SuiteRunner: the command line, read by the
// shell in dir, judged as Run judges a rig's tests, with the environment of
// the rig it is run for.
func (c *Checks) RunLine(ctx context.Context, rig, dir, command string) (application.Checked, error) {
	return c.runLineWith(ctx, rig, dir, command, nil)
}

// Checks satisfies the port a nightly timing run's command line is run through.
var _ application.TimingRunner = (*Checks)(nil)

// RunTiming implements application.TimingRunner: RunLine with env added after
// the rig's own, so that MW_TIMING=only takes the place of the MW_TIMING=skip
// every other run of the rig's tests is given.
func (c *Checks) RunTiming(ctx context.Context, rig, dir, command string, env map[string]string) (application.Checked, error) {
	return c.runLineWith(ctx, rig, dir, command, env)
}

func (c *Checks) runLineWith(ctx context.Context, rig, dir, command string, extra map[string]string) (application.Checked, error) {
	if dir == "" {
		return application.Checked{}, fmt.Errorf("running `%s`: where?", command)
	}
	if _, err := os.Stat(dir); err != nil {
		return application.Checked{}, fmt.Errorf("running `%s`: %w", command, err)
	}

	env := c.commandEnv(rig)
	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env = append(env, name+"="+extra[name])
	}
	output, err := runLine(ctx, c.shell, command, dir, c.nice, env)
	checked := application.Checked{Command: command, Output: output, Passed: err == nil}
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
	return application.Checked{}, fmt.Errorf("running `%s` in %s: %w: %s", command, dir, err, application.RecentLines(output, 10))
}

// killGrace is how long a command that has been told to stop is given to let go
// of what it printed before mw stops waiting for it.
const killGrace = time.Second

// termGrace is how long a cancelled command is given to stop on SIGTERM, and to
// stop what it started in groups of its own, before its group is killed.
var termGrace = 10 * time.Second

// runLine reads a command line with the shell in dir, and reports everything it
// printed, both streams in the order they came. It is the one way this package
// runs a rig's own command line, for its tests and for what follows a landing
// alike. A context that ends stops the command and everything it started, not
// only the shell: `make` and the compiler under it are the ones still running
// when a limit is reached.
//
// The command leaves nothing running when it ends, however it ends: its group
// and everything that carries its mark are killed, so a preview server
// Playwright started in a group of its own does not hold the port the next run
// needs (mw-gq6.625). A rig that means to leave a server running starts it
// through its service manager, not as a child of its command.
func runLine(ctx context.Context, shell, command, dir string, nice int, env []string) (string, error) {
	argv := []string{shell, "-c", command}
	if nice > 0 {
		argv = append([]string{"nice", "-n", strconv.Itoa(nice)}, argv...)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	// Nobody is at the keyboard: a test that asks git for a password would wait
	// there forever in a pane nobody is watching.
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A cancelled command is asked to stop first: Playwright starts its webServer
	// in a group of its own and stops it only on its own exit path, so a SIGKILL
	// leaves that server running (mw-gq6.410). The group is killed once the grace
	// is up, or as soon as the command has returned, whichever comes first.
	grace := termGrace
	var killer *time.Timer
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		err := syscall.Kill(-pgid, syscall.SIGTERM)
		killer = time.AfterFunc(grace, func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
		return err
	}
	cmd.WaitDelay = grace + killGrace

	// The mark finds what left the group. The output is read through a pipe of
	// mw's own, so that a leftover holding it open does not keep the wait from
	// ending: the kill after it is what closes it.
	mark := newRunMark()
	cmd.Env = append(cmd.Env, mark+"=1")
	read, write, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer read.Close()
	cmd.Stdout, cmd.Stderr = write, write
	if err := cmd.Start(); err != nil {
		write.Close()
		return "", err
	}
	write.Close()
	var out bytes.Buffer
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(&out, read)
		close(copied)
	}()

	err = cmd.Wait()
	if killer != nil {
		killer.Stop()
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	reapMarked(mark)
	// What cleared its environment and left the group is not found by its mark:
	// the output is not waited for past the grace on its account.
	select {
	case <-copied:
	case <-time.After(killGrace):
		read.Close()
		<-copied
	}
	return out.String(), err
}
