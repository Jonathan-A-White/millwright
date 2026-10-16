// Package suiteproc runs a rig's nightly suite in a process apart from the
// follower that began it, so that the follower stopping (every self-update
// restarts it, and systemd takes the unit's whole control group with it) does
// not end the suite, and a follower that comes back finds it and waits for it.
package suiteproc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Processes implements application.SuiteProcesses. A suite's state is three
// files in Dir/<rig>: pid (the shell that runs it), status (its exit status,
// written when it ends) and log (what it printed).
type Processes struct {
	// Dir is where the state of the suites is kept.
	Dir string
	// Program is the mw binary the suite is run with.
	Program string
	// Scope is the systemd-run that puts the suite in a scope of its own, outside
	// the follower's control group, which the unit's stop takes whole. Empty looks
	// for systemd-run on the PATH; without one (or a user manager) the suite is
	// only a session of its own, which a stop by control group would still take.
	Scope string
	// Poll is how often a suite is looked at while waited for; zero is a second.
	Poll time.Duration
	// BeginWait is how long Begin waits to see the suite's pid; zero is 10 s.
	BeginWait time.Duration
}

var _ application.SuiteProcesses = (*Processes)(nil)

func (p *Processes) dir(rig string) string { return filepath.Join(p.Dir, rig) }

func (p *Processes) poll() time.Duration {
	if p.Poll > 0 {
		return p.Poll
	}
	return time.Second
}

// script is what the process runs: it notes its pid, runs the suite, and notes
// how it ended. The status file is written whole and then moved into place, so
// it is never half read.
const script = `echo $$ > "$1/pid"; "$2" suite "$3" --file >> "$1/log" 2>&1; echo $? > "$1/status.tmp"; mv "$1/status.tmp" "$1/status"`

// Begin implements application.SuiteProcesses.
func (p *Processes) Begin(ctx context.Context, rig string) error {
	dir := p.dir(rig)
	if running, _ := p.running(dir); running {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range []string{"pid", "status", "status.tmp"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	args := []string{"sh", "-c", script, "mw-suite", dir, p.Program, rig}
	if scope := p.scope(); scope != "" {
		args = append([]string{scope, "--user", "--scope", "--collect", "--quiet"}, args...)
	}
	// Not CommandContext: the follower's context ending must leave it be.
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the suite of %s: %w", rig, err)
	}
	go cmd.Wait() // reaps it if this follower outlives it

	wait := p.BeginWait
	if wait <= 0 {
		wait = 10 * time.Second
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "pid")); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return fmt.Errorf("the suite of %s did not say it had started (%s)", rig, filepath.Join(dir, "log"))
}

// scope is the systemd-run to put the suite in a scope with, or "".
func (p *Processes) scope() string {
	if p.Scope != "" {
		return p.Scope
	}
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return ""
	}
	// A host without a user manager has systemd-run and nothing for it to ask.
	if out, err := exec.Command("systemctl", "--user", "is-system-running").Output(); err != nil && len(out) == 0 {
		return ""
	}
	return path
}

// running says whether the suite whose state is in dir is still running: its
// pid is the shell that was begun for it, not another process that has the
// number now.
func (p *Processes) running(dir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "status")); err == nil {
		return false, nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return false, nil
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(cmdline), dir), nil
}

// Wait implements application.SuiteProcesses.
func (p *Processes) Wait(ctx context.Context, rig string) error {
	dir := p.dir(rig)
	for {
		running, _ := p.running(dir)
		if !running {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.poll()):
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return fmt.Errorf("the suite of %s ended without saying how (see %s)", rig, filepath.Join(dir, "log"))
	}
	if code := strings.TrimSpace(string(raw)); code != "0" {
		return fmt.Errorf("the suite of %s failed on its main (exit status %s; see %s)", rig, code, filepath.Join(dir, "log"))
	}
	return nil
}
