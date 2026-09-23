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
// it prints an empty value, standing in for one systemd has never loaded) and
// logs `--user daemon-reload` calls to a file this test can read back.
func fakeSystemctl(t *testing.T, needs map[string]string) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")

	var cases strings.Builder
	for unit, value := range needs {
		fmt.Fprintf(&cases, "  %q) echo NeedDaemonReload=%s ;;\n", unit, value)
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = daemon-reload ]; then
  exit 0
fi
if [ "$1" = --user ] && [ "$2" = show ]; then
  case "$3" in
%s  *) echo "NeedDaemonReload=" ;;
  esac
  exit 0
fi
exit 1
`, callLog, cases.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func TestTheDaemonReloadProbeParsesYes(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"})
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
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "no"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: program}

	verdict, _ := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s", verdict)
	}
}

func TestTheDaemonReloadProbeCallsAMissingUnitCannotTell(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{})
	check := &doctor.DaemonReload{Units: []string{"mw-frobnicate.service"}, Program: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheDaemonReloadProbePrefersFaultyOverCannotTell(t *testing.T) {
	program, _ := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"})
	check := &doctor.DaemonReload{Units: []string{"mw-dispatch.service", "mw-frobnicate.service"}, Program: program}

	verdict, _ := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty to win over cannot-tell, got %s", verdict)
	}
}

func TestTheDaemonReloadCureRunsSystemctlDaemonReload(t *testing.T) {
	program, calls := fakeSystemctl(t, map[string]string{"mw-dispatch.service": "yes"})
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

func TestTheDaemonReloadDamperIsTenMinutesCapThree(t *testing.T) {
	check := &doctor.DaemonReload{}
	wait, capPerEpisode := check.Damper()
	if wait != doctor.DaemonReloadWait || capPerEpisode != doctor.DaemonReloadCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.DaemonReloadWait, doctor.DaemonReloadCap, wait, capPerEpisode)
	}
}
