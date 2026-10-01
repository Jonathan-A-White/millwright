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

// Harness counts processes whose name is Name, read from the comm file of each
// numbered directory of Proc.
type Harness struct {
	// Proc is the process table; empty is /proc.
	Proc string
	// Name is the process name counted; empty is claude, as `pgrep -c claude`.
	Name string
}

// Count is how many processes are named Name now.
func (h Harness) Count(context.Context) (int, error) {
	proc, name := h.Proc, h.Name
	if proc == "" {
		proc = "/proc"
	}
	if name == "" {
		name = "claude"
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
		if err == nil && strings.TrimSpace(string(comm)) == name {
			n++
		}
	}
	return n, nil
}
