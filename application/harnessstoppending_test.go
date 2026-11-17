package application

import (
	"strconv"
	"testing"
	"time"
)

// mw-gq6.1382: a story waiting for the time its harness stop named is not
// refused open; one whose time has passed, or that carries no mark, is.
func TestAHarnessStopWhoseTimeHasNotComeIsPending(t *testing.T) {
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	label := func(at time.Time) []string {
		return []string{"run:blocked", HarnessStopState + ":" + strconv.FormatInt(at.Unix(), 10)}
	}
	cases := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"the time is ahead", label(now.Add(time.Hour)), true},
		{"the time has come", label(now.Add(-time.Minute)), false},
		{"no mark", []string{"run:blocked"}, false},
		{"a mark that is no time", []string{HarnessStopState + ":soon"}, false},
	}
	for _, tc := range cases {
		if got := harnessStopPending(tc.labels, now); got != tc.want {
			t.Errorf("%s: harnessStopPending = %v, want %v", tc.name, got, tc.want)
		}
	}
}
