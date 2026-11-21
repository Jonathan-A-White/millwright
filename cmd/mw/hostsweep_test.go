package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unitLines are the lines of a unit file that are not comments or blank.
func unitLines(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../contrib/systemd", name))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func hasLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestHostSweepTimerIsInstalledLikeTheDoctorsTimer(t *testing.T) {
	doctorService, sweepService := unitLines(t, "mw-doctor.service"), unitLines(t, "mw-host-sweep.service")
	doctorTimer, sweepTimer := unitLines(t, "mw-doctor.timer"), unitLines(t, "mw-host-sweep.timer")

	// The shape of the doctor's unit: a oneshot that mails the Mayor when it
	// fails, takes the host's two environment files and runs the one fixed mw.
	for _, shared := range []string{
		"OnFailure=mw-unit-failed@%n.service", "Type=oneshot",
		"EnvironmentFile=-%h/.config/mw/dispatch.env", "EnvironmentFile=-%h/.config/mw/beads.env",
		"OOMScoreAdjust=500",
	} {
		if !hasLine(doctorService, shared) || !hasLine(sweepService, shared) {
			t.Errorf("the doctor's service and the sweep's do not both say %q", shared)
		}
	}
	if !hasLine(sweepService, "ExecStart=/usr/bin/env %h/.local/bin/mw host sweep --timer") {
		t.Errorf("the sweep's service does not run mw host sweep --timer: %v", sweepService)
	}
	if !strings.Contains(strings.Join(sweepService, "\n"), "TimeoutStartSec=") {
		t.Error("the sweep's service has no start timeout")
	}
	for _, shared := range []string{"Persistent=false", "WakeSystem=false", "[Install]", "WantedBy=timers.target", "[Timer]", "[Unit]"} {
		if !hasLine(doctorTimer, shared) || !hasLine(sweepTimer, shared) {
			t.Errorf("the doctor's timer and the sweep's do not both say %q", shared)
		}
	}
	// Daily and at boot, and no more often.
	if !hasLine(sweepTimer, "OnCalendar=*-*-* 04:40:00 UTC") || !hasLine(sweepTimer, "OnBootSec=15min") {
		t.Errorf("the sweep's timer is not daily and at boot: %v", sweepTimer)
	}

	// install-units.sh finds the pair by itself and links it like any other.
	home := t.TempDir()
	script, err := filepath.Abs("../../scripts/install-units.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script, "--dry-run", "--enable", "mw-host-sweep")
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install-units.sh refused the pair: %v\n%s", err, out)
	}
	for _, want := range []string{"mw-host-sweep.service", "mw-host-sweep.timer", "enable --now mw-host-sweep.timer"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("install-units.sh's dry run does not say %q:\n%s", want, out)
		}
	}
}

func TestTheTimersFirstRunOnAHostOnlyLists(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "state", "listed")
	if dry, first := hostSweepDry(mark, false, true); !dry || !first {
		t.Errorf("the timer's first run: dry=%v first=%v, want both", dry, first)
	}
	if dry, _ := hostSweepDry(mark, false, false); dry {
		t.Error("a person's run without --dry-run only listed")
	}
	if dry, first := hostSweepDry(mark, true, false); !dry || first {
		t.Errorf("--dry-run: dry=%v first=%v, want a dry run that keeps no mark", dry, first)
	}
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte("listed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dry, _ := hostSweepDry(mark, false, true); dry {
		t.Error("the timer's second run only listed")
	}
}
