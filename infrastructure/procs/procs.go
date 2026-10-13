// Package procs counts the processes of a program alive on this host, from
// /proc: what mw status shows as the harness process count. It reads, and
// starts and signals nothing.
package procs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Harness counts the processes of one user whose name is Name, read from the
// comm and status files of each numbered directory of Proc. Another Linux user's
// processes on the same host are not this user's factory (mw-9rp4hu.1).
type Harness struct {
	// Proc is the process table; empty is /proc.
	Proc string
	// Name is the process name counted; empty is claude, as `pgrep -c claude`.
	Name string
	// Uid is whose processes are counted; nil is os.Getuid, the user running mw.
	Uid func() int
}

// Count is how many processes of the current user are named Name now.
func (h Harness) Count(context.Context) (int, error) {
	proc, name := h.Proc, h.Name
	if proc == "" {
		proc = "/proc"
	}
	if name == "" {
		name = "claude"
	}
	uid := os.Getuid()
	if h.Uid != nil {
		uid = h.Uid()
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		// A process that ended between the listing and the read is not an error.
		comm, err := os.ReadFile(filepath.Join(proc, e.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == name && ownedBy(filepath.Join(proc, e.Name(), "status"), uid) {
			n++
		}
	}
	return n, nil
}

// ownedBy is whether the status file at path gives uid as the process's real
// uid, the first number on its Uid line. A status that cannot be read, or has
// no such line, is not the user's: the process ended, or is not to be counted.
func ownedBy(path string, uid int) bool {
	status, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(status), "\n") {
		rest, ok := strings.CutPrefix(line, "Uid:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return false
		}
		real, err := strconv.Atoi(fields[0])
		return err == nil && real == uid
	}
	return false
}
