package procs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// stopGrace is how long a process is given to end on TERM before it is killed.
const stopGrace = 5 * time.Second

// stopPoll is how often a process told to end is looked at again.
const stopPoll = 100 * time.Millisecond

// Table is the running user's processes, read from Proc, and the way to stop
// them (application.UserProcesses): what mw next sweeps a Tester story's
// scratch directory with (mw-gq6.409).
type Table struct {
	// Proc is the process table; empty is /proc.
	Proc string
	// Uid is whose processes are listed; nil is os.Getuid.
	Uid func() int
	// Self is the process whose own ancestry is never listed; zero is os.Getpid.
	Self int
	// Grace is how long Stop waits after TERM before it kills; zero is five
	// seconds. Sleep waits d between looks; nil is time.Sleep. Signal sends sig
	// to pid; nil is syscall.Kill. Tests replace the last two.
	Grace  time.Duration
	Sleep  func(d time.Duration)
	Signal func(pid int, sig syscall.Signal) error
}

// Table satisfies the port.
var _ application.UserProcesses = Table{}

func (t Table) proc() string {
	if t.Proc == "" {
		return "/proc"
	}
	return t.Proc
}

// List is the user's processes with their command line and working directory,
// without the process mw runs as or any process it was started from. A process
// that ended between the listing and the read, or whose working directory is not
// the user's to read, is listed with what could be read of it.
func (t Table) List(context.Context) ([]application.Process, error) {
	uid := os.Getuid()
	if t.Uid != nil {
		uid = t.Uid()
	}
	entries, err := os.ReadDir(t.proc())
	if err != nil {
		return nil, err
	}
	self := t.Self
	if self == 0 {
		self = os.Getpid()
	}
	spared := t.ancestry(self)
	var out []application.Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || spared[pid] {
			continue
		}
		dir := filepath.Join(t.proc(), e.Name())
		if !ownedBy(filepath.Join(dir, "status"), uid) {
			continue
		}
		p := application.Process{Pid: pid}
		if cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(cmdline) > 0 {
			p.Argv = strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		}
		if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
			p.Cwd = strings.TrimSuffix(cwd, " (deleted)")
		}
		out = append(out, p)
	}
	return out, nil
}

// ancestry is pid and every process it descends from, as the status files give
// them, so that a sweep never stops the session it runs in.
func (t Table) ancestry(pid int) map[int]bool {
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		seen[pid] = true
		status, err := os.ReadFile(filepath.Join(t.proc(), strconv.Itoa(pid), "status"))
		if err != nil {
			break
		}
		pid = 0
		for _, line := range strings.Split(string(status), "\n") {
			if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
				pid, _ = strconv.Atoi(strings.TrimSpace(rest))
				break
			}
		}
	}
	return seen
}

// Stop sends pid TERM, looks every tenth of a second for it to be gone, and
// sends KILL when it is still there after the grace. A process gone already, or
// gone by the time it is signalled, is no error.
func (t Table) Stop(_ context.Context, pid int) error {
	signal, sleep, grace := t.Signal, t.Sleep, t.Grace
	if signal == nil {
		signal = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	if grace == 0 {
		grace = stopGrace
	}
	if err := signal(pid, syscall.SIGTERM); err != nil {
		return gone(err)
	}
	for waited := time.Duration(0); waited < grace; waited += stopPoll {
		if !t.alive(pid) {
			return nil
		}
		sleep(stopPoll)
	}
	if !t.alive(pid) {
		return nil
	}
	return gone(signal(pid, syscall.SIGKILL))
}

func gone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// alive is whether pid is still running: its status file is there and does not
// say zombie, which is a process that has ended and waits to be reaped.
func (t Table) alive(pid int) bool {
	status, err := os.ReadFile(filepath.Join(t.proc(), strconv.Itoa(pid), "status"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "State:"); ok {
			return !strings.HasPrefix(strings.TrimSpace(rest), "Z")
		}
	}
	return true
}
