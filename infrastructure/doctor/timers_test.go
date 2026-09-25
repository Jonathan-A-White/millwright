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
// (timer -> true/false; a timer in neither map reads as disabled but
// installed: exit 1, stdout "disabled"), always succeeds `--user start
// <timer>`, and logs every call to a file this test can read back. A timer in
// notInstalled fails is-enabled the way systemctl does for one with no unit
// file at all: exit 1, empty stdout, "No such file or directory" on stderr. A
// timer in broken fails is-enabled for some other, genuine reason, naming no
// unit file and no bus. A timer in noUserManager fails is-enabled the way
// systemctl does when it cannot reach a user manager's bus at all: exit 1,
// empty stdout, "Failed to connect to bus" on stderr.
func fakeTimersSystemctl(t *testing.T, enabled, active map[string]bool, notInstalled, broken, noUserManager []string) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")

	var enabledCases, activeCases, notInstalledCases, brokenCases, noUserManagerCases strings.Builder
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
	for _, timer := range notInstalled {
		fmt.Fprintf(&notInstalledCases, "  %q) echo %q 1>&2; exit 1 ;;\n", timer,
			"Failed to get unit file state for "+timer+": No such file or directory")
	}
	for _, timer := range broken {
		fmt.Fprintf(&brokenCases, "  %q) echo \"Interactive authentication required.\" 1>&2; exit 1 ;;\n", timer)
	}
	for _, timer := range noUserManager {
		fmt.Fprintf(&noUserManagerCases, "  %q) echo \"Failed to connect to bus: No medium found\" 1>&2; exit 1 ;;\n", timer)
	}

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = is-enabled ]; then
  case "$3" in
%s%s%s%s  *) echo disabled; exit 1 ;;
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
`, callLog, notInstalledCases.String(), brokenCases.String(), noUserManagerCases.String(), enabledCases.String(),
		activeCases.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func TestTheTimersProbeIsOKWhenEveryEnabledTimerIsActive(t *testing.T) {
	program, _ := fakeTimersSystemctl(t,
		map[string]bool{"mw-dispatch.timer": true, "mw-millhand-tick.timer": true},
		map[string]bool{"mw-dispatch.timer": true, "mw-millhand-tick.timer": true},
		nil, nil, nil,
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
		nil, nil, nil,
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
		map[string]bool{}, // mw-millhand-review.timer installed but disabled
		map[string]bool{},
		nil, nil, nil,
	)
	check := &doctor.Timers{Units: []string{"mw-millhand-review.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok (skipped, not faulty), got %s (%s)", verdict, reason)
	}
}

func TestTheTimersProbeIsOKNAWhenNoUnitIsInstalled(t *testing.T) {
	program, _ := fakeTimersSystemctl(t, map[string]bool{}, map[string]bool{},
		[]string{"mw-dispatch.timer", "mw-doctor.timer"}, nil, nil)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service", "mw-doctor.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: none of these units is installed here"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
	}
}

func TestTheTimersProbeSkipsANotInstalledTimerInAMixedList(t *testing.T) {
	program, _ := fakeTimersSystemctl(t,
		map[string]bool{"mw-dispatch.timer": true},
		map[string]bool{"mw-dispatch.timer": false},
		[]string{"mw-doctor.timer"}, nil, nil,
	)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service", "mw-doctor.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "mw-dispatch.timer") {
		t.Errorf("expected the reason to name the inactive timer, got %q", reason)
	}
	if strings.Contains(reason, "mw-doctor.timer") {
		t.Errorf("expected the not-installed timer skipped, not named, got %q", reason)
	}
}

func TestTheTimersProbeIsStillCannotTellOnAGenuineFailure(t *testing.T) {
	program, _ := fakeTimersSystemctl(t, map[string]bool{}, map[string]bool{}, nil, []string{"mw-dispatch.timer"}, nil)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheTimersProbeIsOKNAWhenSystemctlCannotReachAUserManager(t *testing.T) {
	program, _ := fakeTimersSystemctl(t, map[string]bool{}, map[string]bool{}, nil, nil, []string{"mw-dispatch.timer"})
	check := &doctor.Timers{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: no user manager on this host"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
	}
}

func TestTheTimersProbeIsOKNAWhenSystemctlCannotReachAUserManagerInAMixedList(t *testing.T) {
	program, _ := fakeTimersSystemctl(t,
		map[string]bool{"mw-dispatch.timer": true},
		map[string]bool{"mw-dispatch.timer": false},
		nil, nil, []string{"mw-doctor.timer"},
	)
	check := &doctor.Timers{Units: []string{"mw-dispatch.service", "mw-doctor.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: no user manager on this host"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
	}
}

func TestTheTimersProbeIsOKNAWhenIsActiveCannotReachAUserManager(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "systemctl-stand-in")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --user ] && [ \"$2\" = is-enabled ]; then echo enabled; exit 0; fi\n" +
		"if [ \"$1\" = --user ] && [ \"$2\" = is-active ]; then echo \"Failed to connect to bus: No medium found\" 1>&2; exit 1; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	check := &doctor.Timers{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: no user manager on this host"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
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
		nil, nil, nil,
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
