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
