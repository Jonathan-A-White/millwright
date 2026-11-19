package google

import (
	"testing"
	"time"
)

// SlowAnswers makes the loopback redirect's handler wait d after it has handed
// the code over and before its answer is written out, as a loaded machine may,
// until the test ends.
func SlowAnswers(t *testing.T, d time.Duration) {
	t.Helper()
	old := answered
	answered = func() { time.Sleep(d) }
	t.Cleanup(func() { answered = old })
}
