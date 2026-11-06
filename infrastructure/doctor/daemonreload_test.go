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

// fakeSystemctl writes a stand-in for systemctl that answers `--user show
// <unit> -p NeedDaemonReload` from needs (unit -> "yes"/"no"; a unit not in
// it prints an empty value, standing in for one systemd has loaded but has
// nothing to say about), `--user show <unit> -p LoadState` "loaded" for any
// unit not in notInstalled, broken or noUserManager, and logs `--user
// daemon-reload` calls to a file this test can read back. A unit in
// notInstalled fails every show call the way systemctl does for one with no
// unit file at all: exit 1, empty stdout, "No such file or directory" on
// stderr. A unit in broken fails every show call for some other, genuine
// reason, naming no unit file and no bus. A unit in noUserManager fails every
// show call the way systemctl does when it cannot reach a user manager's bus
// at all: exit 1, empty stdout, "Failed to connect to bus" on stderr.
func fakeSystemctl(t *testing.T, needs map[string]string, notInstalled, broken, noUserManager []string) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")

	var cases, notInstalledCases, brokenCases, noUserManagerCases strings.Builder
	for unit, value := range needs {
		fmt.Fprintf(&cases, "  %q) echo NeedDaemonReload=%s ;;\n", unit, value)
	}
	for _, unit := range notInstalled {
		fmt.Fprintf(&notInstalledCases, "  %q) echo %q 1>&2; exit 1 ;;\n", unit,
			"Failed to get properties: Unit "+unit+" could not be found: No such file or directory")
	}
	for _, unit := range broken {
		fmt.Fprintf(&brokenCases, "  %q) echo \"Interactive authentication required.\" 1>&2; exit 1 ;;\n", unit)
	}
	for _, unit := range noUserManager {
		fmt.Fprintf(&noUserManagerCases, "  %q) echo \"Failed to connect to bus: No medium found\" 1>&2; exit 1 ;;\n", unit)
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = daemon-reload ]; then
  exit 0
fi
if [ "$1" = --user ] && [ "$2" = show ]; then
  unit="$3"
  case "$unit" in
%s%s%s  esac
  if [ "$5" = LoadState ]; then
    echo LoadState=loaded
    exit 0
  fi
  case "$unit" in
%s  *) echo "NeedDaemonReload=" ;;
  esac
  exit 0
fi
exit 1
`, callLog, notInstalledCases.String(), brokenCases.String(), noUserManagerCases.String(), cases.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func TestTheDaemonReloadProbeParsesYes(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"}, nil, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "mw-dispatch.service") {
		t.Errorf("expected the reason to name the unit, got %q", reason)
	}
}

func TestTheDaemonReloadProbeParsesNo(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "no"}, nil, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, _ := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s", verdict)
	}
}

func TestTheDaemonReloadProbeCallsAMissingUnitCannotTell(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{}, nil, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-frobnicate.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheDaemonReloadProbePrefersFaultyOverCannotTell(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"}, nil, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service", "mw-frobnicate.service"}, Program: program}

	verdict, _ := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty to win over cannot-tell, got %s", verdict)
	}
}

func TestTheDaemonReloadCureRunsSystemctlDaemonReload(t *testing.T) {
	program, calls := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"}, nil, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("reading the call log: %v", err)
	}
	if !strings.Contains(string(data), "--user daemon-reload") {
		t.Errorf("expected systemctl --user daemon-reload to have run, calls were:\n%s", data)
	}
}

func TestTheDaemonReloadProbeIsOKWhenNoUnitIsInstalled(t *testing.T) {
	program, _ := fakeSystemctl(t, nil, []string{"mw-dispatch.service", "mw-doctor.service"}, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service", "mw-doctor.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: none of these units is installed here"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
	}
}

func TestTheDaemonReloadProbeSkipsANotInstalledUnitInAMixedList(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"}, []string{"mw-doctor.service"}, nil, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service", "mw-doctor.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "mw-dispatch.service") {
		t.Errorf("expected the reason to name the unit needing a reload, got %q", reason)
	}
	if strings.Contains(reason, "mw-doctor.service") {
		t.Errorf("expected the not-installed unit skipped, not named, got %q", reason)
	}
}

func TestTheDaemonReloadProbeIsStillCannotTellOnAGenuineFailure(t *testing.T) {
	program, _ := fakeSystemctl(t, nil, nil, []string{"mw-dispatch.service"}, nil)
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheDaemonReloadProbeIsOKNAWhenSystemctlCannotReachAUserManager(t *testing.T) {
	program, _ := fakeSystemctl(t, nil, nil, nil, []string{"mw-dispatch.service"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: no user manager on this host"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
	}
}

func TestTheDaemonReloadProbeIsOKNAWhenSystemctlCannotReachAUserManagerInAMixedList(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"}, nil, nil, []string{"mw-doctor.service"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service", "mw-doctor.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if want := "n/a: no user manager on this host"; reason != want {
		t.Errorf("expected reason %q, got %q", want, reason)
	}
}

func TestTheDaemonReloadDamperIsTenMinutesCapThree(t *testing.T) {
	check := &doctor.DaemonReload{}
	wait, capPerEpisode := check.Damper()
	if wait != doctor.DaemonReloadWait || capPerEpisode != doctor.DaemonReloadCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.DaemonReloadWait, doctor.DaemonReloadCap, wait, capPerEpisode)
	}
}

// listingSystemctl is a stand-in for a systemctl whose user manager has
// loaded the units in loaded (unit -> yes/no for NeedDaemonReload), so that
// `list-units` names them and a `show` of several answers each; a `show` of
// one unit with -p LoadState or -p NeedDaemonReload says loaded and no.
func listingSystemctl(t *testing.T, loaded map[string]string) (program, callLog string) {
	t.Helper()
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")
	var list, blocks strings.Builder
	for unit, value := range loaded {
		fmt.Fprintf(&list, "echo '%s loaded active running x'\n", unit)
		fmt.Fprintf(&blocks, "echo Id=%s\necho NeedDaemonReload=%s\necho\n", unit, value)
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
case "$*" in
*daemon-reload*) exit 0 ;;
*list-units*)
%s exit 0 ;;
*"-p Id"*)
%s exit 0 ;;
*LoadState*) echo LoadState=loaded; exit 0 ;;
*NeedDaemonReload*) echo NeedDaemonReload=no; exit 0 ;;
esac
exit 1
`, callLog, list.String(), blocks.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func TestTheDaemonReloadProbeAlarmsOnAnyMwUnitThatNeedsAReloadEvenOneNotInTheList(t *testing.T) {
	program, _ := listingSystemctl(t, map[string]string{"mw-postern-mirror.service": "yes", "mw-health.timer": "no"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "mw-postern-mirror.service") {
		t.Errorf("expected the reason to name the unit, got %q", reason)
	}
	if strings.Contains(reason, "mw-health.timer") {
		t.Errorf("expected only the unit needing a reload named, got %q", reason)
	}
	if !strings.Contains(reason, "systemctl --user daemon-reload") {
		t.Errorf("expected the reason to say the one fix line, got %q", reason)
	}
}

func TestTheDaemonReloadProbeNamesAUnitInTheListAndOneBeyondItOnce(t *testing.T) {
	program, _ := listingSystemctl(t, map[string]string{"mw-dispatch.service": "yes", "mw-health.timer": "yes"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	_, reason := check.Probe(context.Background())
	if n := strings.Count(reason, "mw-dispatch.service"); n != 1 {
		t.Errorf("expected mw-dispatch.service named once, got %d in %q", n, reason)
	}
	if !strings.Contains(reason, "mw-health.timer") {
		t.Errorf("expected mw-health.timer named too, got %q", reason)
	}
}

func TestTheDaemonReloadProbeIsOKWhenEveryMwUnitIsCurrent(t *testing.T) {
	program, _ := listingSystemctl(t, map[string]string{"mw-postern-mirror.service": "no"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}
