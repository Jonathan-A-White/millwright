package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// With BASH_MAX_TIMEOUT_MS unset, empty, not a number or no larger than the
// default cap, a foreground check keeps to today's times: 540 seconds of the
// call, nine minutes of waiting, a place kept ten minutes (mw-t5shinh.14).
func TestACheckKeepsToTodaysTimesWithoutALongerCall(t *testing.T) {
	for _, env := range []string{"", "   ", "soon", "55m", "-1", "0", "3300000.5", "600000", "120000"} {
		got := application.CheckCallLimitsFor(env)
		if got.Budget != 540*time.Second || got.Wait != 9*time.Minute || got.Keep != 10*time.Minute {
			t.Errorf("BASH_MAX_TIMEOUT_MS=%q read as %+v, want 540s, 9m, 10m", env, got)
		}
	}
}

// A longer call is the budget less 50 seconds, which every wait may spend; the
// kept place stays ten minutes.
func TestACheckInALongerCallSpendsThatCallLessFiftySeconds(t *testing.T) {
	got := application.CheckCallLimitsFor("3300000")
	want := 3300*time.Second - 50*time.Second
	if got.Budget != want || got.Wait != want || got.Keep != 10*time.Minute {
		t.Errorf("BASH_MAX_TIMEOUT_MS=3300000 read as %+v, want %s, %s, 10m", got, want, want)
	}
	if got := application.CheckCallLimitsFor(" 1800000 "); got.Budget != 1750*time.Second {
		t.Errorf("a padded 1800000 read as %+v", got)
	}
}
