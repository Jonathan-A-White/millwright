package rig

import (
	"testing"
	"time"
)

// SetTermGraceForTest shortens how long a cancelled check is given to stop on
// SIGTERM before it is killed, for the length of one test.
func SetTermGraceForTest(t *testing.T, d time.Duration) {
	t.Helper()
	old := termGrace
	termGrace = d
	t.Cleanup(func() { termGrace = old })
}

// LeaveWritersAloneForTest makes w find the processes holding a worktree but
// end none of them, as on a host where they would not die: for the test of what
// a refusal says about them.
func LeaveWritersAloneForTest(w *Worktrees) {
	w.endProcess = func(int) error { return nil }
	w.writersWait = 100 * time.Millisecond
}
