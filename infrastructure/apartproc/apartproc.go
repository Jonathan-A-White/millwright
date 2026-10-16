// Package apartproc runs the follower's long passes, a rig's nightly suite and
// a rig's landing, each in a process apart from the follower that began it, so
// that the follower stopping (every self-update restarts it, and systemd takes
// the unit's whole control group with it) does not end the pass, and a follower
// that comes back finds it and waits for it (mw-gq6.395, mw-gq6.470).
package apartproc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Processes implements application.SuiteProcesses: the nightly suites, each
// `Program suite <rig> --file`, their state in Dir/<rig>.
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

func (p *Processes) apart() apart {
	return apart{Dir: p.Dir, Scope: p.Scope, Poll: p.Poll, BeginWait: p.BeginWait}
}

// Begin implements application.SuiteProcesses.
func (p *Processes) Begin(ctx context.Context, rig string) error {
	return p.apart().begin(ctx, rig, "the suite of "+rig, p.Program, "suite", rig, "--file")
}

// Wait implements application.SuiteProcesses.
func (p *Processes) Wait(ctx context.Context, rig string) error {
	return p.apart().wait(ctx, rig, "the suite of "+rig, "failed on its main")
}

// Landings implements application.LandProcesses: the follower's landings, each
// `Program land <rig>` (with --one, one story by itself), their state in
// Dir/<rig>. Its fields are those of Processes.
type Landings struct {
	Dir       string
	Program   string
	Scope     string
	Poll      time.Duration
	BeginWait time.Duration
}

var _ application.LandProcesses = (*Landings)(nil)

func (l *Landings) apart() apart {
	return apart{Dir: l.Dir, Scope: l.Scope, Poll: l.Poll, BeginWait: l.BeginWait}
}

// Begin implements application.LandProcesses.
func (l *Landings) Begin(ctx context.Context, rig string, one bool) error {
	args := []string{l.Program, "land", rig}
	if one {
		args = append(args, "--one")
	}
	return l.apart().begin(ctx, rig, "mw land "+rig, args...)
}

// Wait implements application.LandProcesses.
func (l *Landings) Wait(ctx context.Context, rig string) error {
	return l.apart().wait(ctx, rig, "mw land "+rig, "failed")
}

// Pending implements application.LandProcesses.
func (l *Landings) Pending(context.Context) ([]string, error) {
	return l.apart().pending()
}

// apart is one kind of pass run apart from the follower. A pass's state is
// five files in Dir/<name>: pid (the shell that runs it, moved into place once whole), status (its exit
// status, written when it ends), owed (there while no follower has been told how
// it ended), log (what it printed) and log.1 (what the pass before it printed).
type apart struct {
	Dir       string
	Scope     string
	Poll      time.Duration
	BeginWait time.Duration
}

func (a apart) dir(name string) string { return filepath.Join(a.Dir, name) }

func (a apart) poll() time.Duration {
	if a.Poll > 0 {
		return a.Poll
	}
	return time.Second
}

// script is what the process runs: it notes its pid, runs the rest of its
// arguments, and notes how they ended. The pid and status files are written
// whole and then moved into place, so neither is ever read empty or half written
// (a pid read empty was a pass that "ended without saying how", mw-gq6.516).
const script = `d=$1; shift; echo $$ > "$d/pid.tmp"; mv "$d/pid.tmp" "$d/pid"; "$@" >> "$d/log" 2>&1; echo $? > "$d/status.tmp"; mv "$d/status.tmp" "$d/status"`

// begin starts args as the pass name, unless one begun earlier, by this
// follower or another, is still running or has ended with no follower told
// how: that one is the pass, and wait tells it.
func (a apart) begin(ctx context.Context, name, what string, args ...string) error {
	dir := a.dir(name)
	if a.owed(dir) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range []string{"pid", "pid.tmp", "status", "status.tmp", "owed"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(filepath.Join(dir, "log"), filepath.Join(dir, "log.1")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "owed"), nil, 0o644); err != nil {
		return err
	}
	args = append([]string{"sh", "-c", script, "mw-apart", dir}, args...)
	if scope := a.scope(); scope != "" {
		args = append([]string{scope, "--user", "--scope", "--collect", "--quiet"}, args...)
	}
	// Not CommandContext: the follower's context ending must leave it be.
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		os.Remove(filepath.Join(dir, "owed"))
		return fmt.Errorf("starting %s: %w", what, err)
	}
	go cmd.Wait() // reaps it if this follower outlives it

	wait := a.BeginWait
	if wait <= 0 {
		wait = 10 * time.Second
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		// A pid file read empty is one whose number is still being written
		// (a pass begun by an older mw makes it before it fills it): not yet said.
		if raw, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s did not say it had started (%s)", what, filepath.Join(dir, "log"))
}

// owed says whether the pass whose state is in dir is still running, or has
// ended with no follower told how.
func (a apart) owed(dir string) bool {
	if running, _ := a.running(dir); running {
		return true
	}
	_, statusErr := os.Stat(filepath.Join(dir, "status"))
	_, owedErr := os.Stat(filepath.Join(dir, "owed"))
	return statusErr == nil && owedErr == nil
}

// pending names the passes that are owed, in name order.
func (a apart) pending() ([]string, error) {
	entries, err := os.ReadDir(a.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && a.owed(a.dir(e.Name())) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// scope is the systemd-run to put the pass in a scope with, or "".
func (a apart) scope() string {
	if a.Scope != "" {
		return a.Scope
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

// running says whether the pass whose state is in dir is still running: its
// pid is the shell that was begun for it, not another process that has the
// number now.
func (a apart) running(dir string) (bool, error) {
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

// wait returns when the pass name ends: nil if it exited 0, an error saying it
// failed (in the words of failed) or ended without saying how. Once told, the
// pass is no longer owed. A ctx that ends stops the wait, not the pass.
func (a apart) wait(ctx context.Context, name, what, failed string) error {
	dir := a.dir(name)
	for {
		running, _ := a.running(dir)
		if !running {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.poll()):
		}
	}
	defer os.Remove(filepath.Join(dir, "owed"))
	raw, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return fmt.Errorf("%s ended without saying how (see %s)", what, filepath.Join(dir, "log"))
	}
	if code := strings.TrimSpace(string(raw)); code != "0" {
		return fmt.Errorf("%s %s (exit status %s; see %s)", what, failed, code, filepath.Join(dir, "log"))
	}
	return nil
}
