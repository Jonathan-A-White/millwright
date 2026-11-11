package procs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Jonathan-A-White/millwright/application"
)

// Freezer is the process table of one user and the two signals that give way
// sends (application.GiveWayProcesses): SIGSTOP and SIGCONT, to a process whose
// pid AND start time are what the caller saw, or to none.
type Freezer struct {
	// Proc is the process table; empty is /proc.
	Proc string
	// Uid is whose processes are listed; nil is os.Getuid.
	Uid func() int
	// Signal sends sig to pid; nil is syscall.Kill. A test replaces it.
	Signal func(pid int, sig syscall.Signal) error
}

var _ application.GiveWayProcesses = Freezer{}

func (f Freezer) proc() string {
	if f.Proc == "" {
		return "/proc"
	}
	return f.Proc
}

// List is every process of the user: its parent, session, state, start time,
// command line, and whether it sits in a systemd-run scope. A process that ended
// between the listing and the read is left out.
func (f Freezer) List(context.Context) ([]application.GiveWayProcess, error) {
	uid := os.Getuid()
	if f.Uid != nil {
		uid = f.Uid()
	}
	entries, err := os.ReadDir(f.proc())
	if err != nil {
		return nil, err
	}
	var out []application.GiveWayProcess
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(f.proc(), e.Name())
		p, ok := readStat(filepath.Join(dir, "stat"))
		if !ok {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !ownedByUid(info, uid) {
			continue
		}
		p.Pid = pid
		if cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(cmdline) > 0 {
			p.Argv = strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		}
		if cgroup, err := os.ReadFile(filepath.Join(dir, "cgroup")); err == nil {
			p.Scope = inRunScope(string(cgroup))
		}
		out = append(out, p)
	}
	return out, nil
}

func ownedByUid(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

// readStat reads the fields of a /proc/<pid>/stat that give way uses: state,
// parent, session and start time. The command name, in parentheses, may hold
// anything, so the fields are read from after its last closing parenthesis.
func readStat(path string) (application.GiveWayProcess, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return application.GiveWayProcess{}, false
	}
	text := string(raw)
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return application.GiveWayProcess{}, false
	}
	fields := strings.Fields(text[end+1:])
	// state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime
	// stime cutime cstime priority nice threads itrealvalue starttime
	if len(fields) < 20 {
		return application.GiveWayProcess{}, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	sid, err2 := strconv.Atoi(fields[3])
	if err1 != nil || err2 != nil {
		return application.GiveWayProcess{}, false
	}
	return application.GiveWayProcess{Ppid: ppid, Sid: sid, State: fields[0], Start: fields[19]}, true
}

// inRunScope is whether a cgroup file puts the process in a scope that
// systemd-run made (run-r<n>.scope, run-p<n>-i<n>.scope), which is where
// landings, suites and mw-heavy run, outside any tmux pane.
func inRunScope(cgroup string) bool {
	for _, line := range strings.Split(cgroup, "\n") {
		path := line[strings.LastIndex(line, ":")+1:]
		leaf := path[strings.LastIndex(path, "/")+1:]
		if strings.HasPrefix(leaf, "run-") && strings.HasSuffix(leaf, ".scope") {
			return true
		}
	}
	return false
}

// Stop implements application.GiveWayProcesses: SIGSTOP.
func (f Freezer) Stop(_ context.Context, pid int, start string) (bool, error) {
	return f.send(pid, start, syscall.SIGSTOP)
}

// Continue implements application.GiveWayProcesses: SIGCONT.
func (f Freezer) Continue(_ context.Context, pid int, start string) (bool, error) {
	return f.send(pid, start, syscall.SIGCONT)
}

// send signals pid only when it still has the start time the caller saw: a
// process that ended and whose pid another process now has is sent nothing. Some
// microseconds lie between the look and the signal; a SIGCONT to a process not
// stopped is no harm, and a SIGSTOP to a reused pid is the loss a pidfd would
// close, at the price of a kernel the Laptop and the desktop both have.
func (f Freezer) send(pid int, start string, sig syscall.Signal) (bool, error) {
	p, ok := readStat(filepath.Join(f.proc(), strconv.Itoa(pid), "stat"))
	if !ok || p.Start != start || p.State == "Z" || p.State == "X" {
		return false, nil
	}
	signal := f.Signal
	if signal == nil {
		signal = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
	}
	if err := signal(pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
