package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// RunnableProcs is the real application.RunnableTable: it reads every process
// of /proc with its parent, whether it is running (state R, which the load
// average counts), its working directory and its command line, for the
// builder-burn check (mw-gq6.1529).
type RunnableProcs struct {
	// ProcDir is the directory the processes are read from; empty reads "/proc".
	ProcDir string
	// Now is the clock the snapshot is dated by; nil reads time.Now.
	Now func() time.Time
}

var _ application.RunnableTable = RunnableProcs{}

func (p RunnableProcs) proc() string {
	if p.ProcDir != "" {
		return p.ProcDir
	}
	return "/proc"
}

// Snapshot implements application.RunnableTable. A process that ended between
// the listing and the read is left out; one whose directory or command line is
// not the user's to read is kept with what could be read of it.
func (p RunnableProcs) Snapshot(context.Context) (application.RunnableSnapshot, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	entries, err := os.ReadDir(p.proc())
	if err != nil {
		return application.RunnableSnapshot{}, err
	}
	snap := application.RunnableSnapshot{At: now()}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(p.proc(), e.Name())
		stat, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			continue
		}
		ppid, running, ok := parseStat(string(stat))
		if !ok {
			continue
		}
		proc := application.RunnableProcess{Pid: pid, Ppid: ppid, Running: running}
		if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
			proc.Cwd = strings.TrimSuffix(cwd, " (deleted)")
		}
		if raw, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(raw) > 0 {
			proc.Argv = strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		}
		snap.Processes = append(snap.Processes, proc)
	}
	return snap, nil
}

// parseStat reads the parent and the state out of a /proc/<pid>/stat line:
// "pid (name) S ppid ...", where the name may hold spaces and parentheses, so it
// is the last ")" that ends it.
func parseStat(line string) (ppid int, running, ok bool) {
	end := strings.LastIndexByte(line, ')')
	if end < 0 {
		return 0, false, false
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) < 2 {
		return 0, false, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false, false
	}
	return ppid, fields[0] == "R", true
}
