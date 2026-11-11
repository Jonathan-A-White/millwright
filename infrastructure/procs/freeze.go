package procs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

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
	// Renice sets the nice value of one thread; nil is setpriority(2). IoIdle sets
	// one thread's io class to idle; nil is ioprio_set(2). A test replaces them.
	Renice func(tid, nice int) error
	IoIdle func(tid int) error
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
		// Neither is readable for every process, and what is not is left empty.
		if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
			p.Cwd = cwd
		}
		if environ, err := os.ReadFile(filepath.Join(dir, "environ")); err == nil {
			p.Story = storyOf(environ)
		}
		out = append(out, p)
	}
	return out, nil
}

// storyOf is the MW_STORY of an environ file (NAME=value entries, NUL-ended): the
// story a Builder's session sets for everything it starts.
func storyOf(environ []byte) string {
	for _, entry := range strings.Split(string(environ), "\x00") {
		if story, ok := strings.CutPrefix(entry, "MW_STORY="); ok {
			return story
		}
	}
	return ""
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

// Lower implements application.GiveWayProcesses: every thread of the process to
// nice 19 and io class idle (nice is a thread's own, so setting the process's pid
// alone would lower only its first thread). A thread that ended since the look is
// no error; the bool is whether the process was still there to lower.
func (f Freezer) Lower(_ context.Context, pid int, start string) (bool, error) {
	p, ok := readStat(filepath.Join(f.proc(), strconv.Itoa(pid), "stat"))
	if !ok || p.Start != start || p.State == "Z" || p.State == "X" {
		return false, nil
	}
	tids := []int{pid}
	if entries, err := os.ReadDir(filepath.Join(f.proc(), strconv.Itoa(pid), "task")); err == nil {
		tids = tids[:0]
		for _, e := range entries {
			if tid, err := strconv.Atoi(e.Name()); err == nil {
				tids = append(tids, tid)
			}
		}
		if len(tids) == 0 {
			tids = []int{pid}
		}
	}
	renice, ioIdle := f.Renice, f.IoIdle
	if renice == nil {
		renice = func(tid, nice int) error { return syscall.Setpriority(syscall.PRIO_PROCESS, tid, nice) }
	}
	if ioIdle == nil {
		ioIdle = setIoIdle
	}
	lowered := false
	for _, tid := range tids {
		for _, set := range []func() error{func() error { return renice(tid, 19) }, func() error { return ioIdle(tid) }} {
			err := set()
			switch {
			case err == nil:
				lowered = true
			case errors.Is(err, syscall.ESRCH):
			default:
				return false, err
			}
		}
	}
	return lowered, nil
}

// Linux's ioprio_set(2): who is a thread, the class idle is 3 in the bits above 13.
const (
	ioprioWhoProcess = 1
	ioprioClassShift = 13
	ioprioClassIdle  = 3
)

func setIoIdle(tid int) error {
	_, _, errno := syscall.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioClassIdle<<ioprioClassShift)
	if errno != 0 {
		return errno
	}
	return nil
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
