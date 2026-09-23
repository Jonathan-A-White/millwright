package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// fakeTimersSystemctl writes a stand-in for systemctl that answers `--user
// is-enabled <timer>` and `--user is-active <timer>` from enabled and active
// (timer -> true/false; a timer in neither map reads as not enabled, the way
// a timer never installed on this host would), always succeeds `--user start
// <timer>`, and logs every call to a file this test can read back.
func fakeTimersSystemctl(t *testing.T, enabled, active map[string]bool) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")

	var enabledCases, activeCases strings.Builder
	for timer, ok := range enabled {
		if ok {
			fmt.Fprintf(&enabledCases, "  %q) echo enabled; exit 0 ;;\n", timer)
		}
	}
	for timer, ok := range active {
		if ok {
			fmt.Fprintf(&activeCases, "  %q) echo active; exit 0 ;;\n", timer)
		}
	}

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = is-enabled ]; then
  case "$3" in
%s  *) echo disabled; exit 1 ;;
  esac
fi
if [ "$1" = --user ] && [ "$2" = is-active ]; then
  case "$3" in
%s  *) echo inactive; exit 3 ;;
  esac
fi
if [ "$1" = --user ] && [ "$2" = start ]; then
  exit 0
fi
exit 1
`, callLog, enabledCases.String(), activeCases.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func TestTheTimersProbeIsOKWhenEveryEnabledTimerIsActive(t *testing.T) {
	program, _ := fakeTimersSystemctl(t,
		map[string]bool{"mw-dispatch.timer": true, "mw-millhand-tick.timer": true},
		map[string]bool{"mw-dispatch.timer": true, "mw-millhand-tick.timer": true},
	)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service", "mw-millhand-tick.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheTimersProbeIsFaultyWhenAnEnabledTimerIsInactive(t *testing.T) {
	program, _ := fakeTimersSystemctl(t,
		map[string]bool{"mw-dispatch.timer": true},
		map[string]bool{"mw-dispatch.timer": false},
	)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "mw-dispatch.timer") {
		t.Errorf("expected the reason to name the timer, got %q", reason)
	}
}

func TestTheTimersProbeSkipsATimerThatIsNotEnabled(t *testing.T) {
	program, _ := fakeTimersSystemctl(t,
		map[string]bool{}, // mw-millhand-review.timer not installed on this host
		map[string]bool{},
	)
	check := &doctor.Timers{Units: []string{"mw-millhand-review.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok (skipped, not faulty), got %s (%s)", verdict, reason)
	}
}

func TestTheTimersProbeCannotTellWhenSystemctlIsAbsent(t *testing.T) {
	check := &doctor.Timers{Units: []string{"mw-dispatch.service"}, Program: filepath.Join(t.TempDir(), "no-such-systemctl")}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheTimersCureStartsOnlyTheFaultyTimer(t *testing.T) {
	program, calls := fakeTimersSystemctl(t,
		map[string]bool{"mw-dispatch.timer": true, "mw-millhand-tick.timer": true},
		map[string]bool{"mw-dispatch.timer": false, "mw-millhand-tick.timer": true},
	)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service", "mw-millhand-tick.service"}, Program: program}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("reading the call log: %v", err)
	}
	if !strings.Contains(string(data), "--user start mw-dispatch.timer") {
		t.Errorf("expected systemctl --user start mw-dispatch.timer to have run, calls were:\n%s", data)
	}
	if strings.Contains(string(data), "--user start mw-millhand-tick.timer") {
		t.Errorf("expected the already-active timer not to have been started, calls were:\n%s", data)
	}

	if way := check.WayBack(); way != "systemctl --user stop mw-dispatch.timer" {
		t.Fatalf("expected the way back to name the timer actually started, got %q", way)
	}
}

func TestTheTimersWayBackBeforeAnyCureSaysThereIsNothingToStop(t *testing.T) {
	check := &doctor.Timers{}
	if way := check.WayBack(); !strings.Contains(way, "nothing to stop") {
		t.Fatalf("expected a way back saying there is nothing to stop yet, got %q", way)
	}
}

func TestTheTimersDamperIsThirtyMinutesCapThree(t *testing.T) {
	check := &doctor.Timers{}
	wait, capPerEpisode := check.Damper()
	if wait != doctor.TimersDamperWait || capPerEpisode != doctor.TimersDamperCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.TimersDamperWait, doctor.TimersDamperCap, wait, capPerEpisode)
	}
}
