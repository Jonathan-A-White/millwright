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

// userUnit is one unit the stand-in systemctl knows: the ExecStart argv and
// the EnvironmentFiles it prints, as `systemctl --user show` words them.
type userUnit struct {
	argv  string
	files []string
}

// fakeUnitSystemctl writes a stand-in for systemctl that lists the units
// (list-units and list-unit-files, with the columns the real ones print) and
// answers `--user show -p ExecStart -p EnvironmentFiles <unit>` for them.
func fakeUnitSystemctl(t *testing.T, units map[string]userUnit) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	var list, show strings.Builder
	for name, u := range units {
		fmt.Fprintf(&list, "%s loaded inactive dead A description\n", name)
		fmt.Fprintf(&show, "  %q)\n    echo 'ExecStart={ path=%s ; argv[]=%s ; ignore_errors=no ; code=exited ; status=0 }'\n", name, strings.Fields(u.argv)[0], u.argv)
		for _, f := range u.files {
			fmt.Fprintf(&show, "    echo 'EnvironmentFiles=%s (ignore_errors=yes)'\n", f)
		}
		show.WriteString("    ;;\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "list"), []byte(list.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "systemctl-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "--user list-units" ] || [ "$1 $2" = "--user list-unit-files" ]; then
  cat %q
  exit 0
fi
if [ "$1 $2" = "--user show" ]; then
  case "$7" in
%s  esac
  exit 0
fi
exit 1
`, filepath.Join(dir, "list"), show.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program
}

const beadsEnv = "/home/someone/.config/mw/beads.env"

func TestUnitEnvFlagsAUserUnitThatRunsMwWithoutBeadsEnv(t *testing.T) {
	program := fakeUnitSystemctl(t, map[string]userUnit{
		"mw-resume-2026-10-29.service": {argv: "/home/someone/bin/mw resume"},
		"mw-dispatch.service":          {argv: "/usr/bin/env mw dispatch", files: []string{beadsEnv}},
	})
	check := &doctor.UnitEnv{Program: program}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict %v (%q), want faulty", verdict, reason)
	}
	if !strings.Contains(reason, "mw-resume-2026-10-29.service") || strings.Contains(reason, "mw-dispatch.service") {
		t.Errorf("reason %q should name the resume unit and not the dispatch one", reason)
	}
}

func TestUnitEnvIsCleanWhenEveryUnitThatRunsMwCarriesIt(t *testing.T) {
	program := fakeUnitSystemctl(t, map[string]userUnit{
		"mw-resume-2026-10-29.service": {argv: "/home/someone/bin/mw resume", files: []string{beadsEnv}},
		"mw-dispatch.service":          {argv: "/usr/bin/env mw dispatch", files: []string{"/home/someone/.config/mw/dispatch.env", beadsEnv}},
		"mw-health.service":            {argv: "/home/someone/bin/mw-health", files: []string{beadsEnv}},
		// not mw: a scorer, a tmux seat, another program's unit
		"mw-scorer.service":    {argv: "/home/someone/venv/bin/python -m mw_scorer", files: []string{"/home/someone/.config/mw/scorer.env"}},
		"mw-seat-tmux.service": {argv: "/bin/sh -c tmux"},
		// the one unit let off: it touches no tracker
		"mw-postern-mirror.service": {argv: "/usr/bin/env mw postern mirror", files: []string{"/home/someone/.config/mw/dispatch.env"}},
	})
	check := &doctor.UnitEnv{Program: program}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("verdict %v (%q), want ok", verdict, reason)
	}
}

func TestUnitEnvAlarmsTheGovernorNamingTheUnit(t *testing.T) {
	program := fakeUnitSystemctl(t, map[string]userUnit{
		"mw-resume-2026-10-29.service": {argv: "/home/someone/bin/mw resume"},
	})
	var sent string
	check := &doctor.UnitEnv{Program: program, Alarm: func(_ context.Context, text string) (uint64, error) { sent = text; return 0, nil }}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "mw-resume-2026-10-29.service") || !strings.Contains(sent, "beads.env") {
		t.Errorf("alarm %q does not name the unit and the file", sent)
	}
}
