package application

import (
	"context"
	"fmt"
	"strings"
)

// Process is one process of the user running mw, as its table shows it.
type Process struct {
	Pid int
	// Argv is the command line, one word each; Cwd is where the process is, with
	// no " (deleted)" mark, as the table reads it.
	Argv []string
	Cwd  string
}

// UserProcesses is this host's table of the running user's processes. List
// leaves out mw's own process and the ones it was started from, so that sweeping
// never stops the session that is sweeping.
type UserProcesses interface {
	List(ctx context.Context) ([]Process, error)
	// Stop ends one process: TERM, then KILL when it is still there after five
	// seconds. A process that is gone already is no error.
	Stop(ctx context.Context, pid int) error
}

// TesterDir is the directory everything a Tester story makes lives under.
func TesterDir(story string) string { return "/tmp/tester-" + story + "/" }

// Under reports whether the process works under dir, a directory with its
// trailing slash: its working directory is dir or inside it, or its command
// line names a path inside it. A sibling that only shares a prefix of dir's
// name (/tmp/tester-mw-x.10/ against /tmp/tester-mw-x.1/) is not under it.
func (p Process) Under(dir string) bool {
	bare := strings.TrimSuffix(dir, "/")
	cwd := strings.TrimSuffix(p.Cwd, " (deleted)")
	if cwd == bare || strings.HasPrefix(cwd, dir) {
		return true
	}
	for _, arg := range p.Argv {
		if arg == bare || strings.Contains(arg, dir) {
			return true
		}
	}
	return false
}

// sweepTester stops what a Tester story's session left running under its
// scratch directory, once its close-out has finished with the story whatever
// the outcome: a preview server started in the background outlives the headless
// session, keeps its port, and fails any later preview on it (mw-gq6.409).
// Nothing it does can fail a close-out: a table that cannot be read, or a
// process that will not stop, is a note on the report.
func (n Next) sweepTester(ctx context.Context, report *NextReport) {
	if n.Leftovers == nil || !report.tester {
		return
	}
	dir := TesterDir(report.StoryID)
	table, err := n.Leftovers.List(ctx)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the processes left under %s could not be looked for: %v", dir, err))
		return
	}
	for _, p := range table {
		if !p.Under(dir) {
			continue
		}
		if err := n.Leftovers.Stop(ctx, p.Pid); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("process %d under %s could not be stopped: %v", p.Pid, dir, err))
			continue
		}
		report.Swept++
	}
	report.SweptDir = dir
}
