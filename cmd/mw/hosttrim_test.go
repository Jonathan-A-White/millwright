package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// hostTrimStates swaps what each host answers to the probe, and which hosts
// there are: here, and each other host reached by "ssh <name>".
func hostTrimStates(t *testing.T, here string, states map[string]string) {
	t.Helper()
	realHosts, realProbe := hostTrimHostsFn, hostTrimProbe
	hostTrimHostsFn = func() (string, map[string]string, error) {
		reach := map[string]string{}
		for name := range states {
			if name != here {
				reach[name] = "ssh " + name
			}
		}
		return here, reach, nil
	}
	hostTrimProbe = func(_ context.Context, host, _ string) (string, error) { return states[host], nil }
	t.Cleanup(func() { hostTrimHostsFn, hostTrimProbe = realHosts, realProbe })
}

func TestHostTrimSetupPrintsTheOnePowerShellLine(t *testing.T) {
	hostTrimStates(t, "laptop", map[string]string{"laptop": doctor.WSLTrimHasDropIn})
	out, err := runMw(t, "host", "trim-setup")
	if err != nil {
		t.Fatalf("mw host trim-setup failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, doctor.WSLTrimPowerShellLine()+"\n") {
		t.Errorf("expected the PowerShell line on its own line, got %q", out)
	}
	if !strings.Contains(out, "wsl -d Ubuntu-24.04 -u root -- sh -c '") {
		t.Errorf("expected the wsl -u root sh -c form, got %q", out)
	}
}

// With a bead, each WSL host that lacks the drop-in gets one hands step as
// root; a host that has it, and a host that is not WSL, get none.
func TestHostTrimSetupFilesAHandsStepForEachWSLHostLackingTheDropIn(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	fakeHandsPush(t, nil)
	log := bdRecording(t, `[{"id": "mw-gq6.1020", "title": "Trim", "status": "open", "issue_type": "task"}]`)
	hostTrimStates(t, "laptop", map[string]string{
		"laptop":  doctor.WSLTrimLacksDropIn,
		"desktop": doctor.WSLTrimLacksDropIn,
		"done":    doctor.WSLTrimHasDropIn,
		"vps":     doctor.WSLTrimNotWSL,
	})

	out, err := runMw(t, "host", "trim-setup", "--bead", "mw-gq6.1020")
	if err != nil {
		t.Fatalf("mw host trim-setup --bead failed: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	for _, host := range []string{"laptop", "desktop"} {
		want := "HANDS STEP wsl-fstrim-" + host + " on " + host + " as root:"
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected a step for %s (%q), got:\n%s", host, want, calls)
		}
	}
	for _, host := range []string{"done", "vps"} {
		if strings.Contains(string(calls), "wsl-fstrim-"+host+" ") {
			t.Errorf("expected no step for %s, got:\n%s", host, calls)
		}
	}
	if !strings.Contains(string(calls), doctor.WSLTrimRootScript()[:40]) {
		t.Errorf("expected the step to carry the root script, got:\n%s", calls)
	}
	if !strings.Contains(out, "vps: not WSL") || !strings.Contains(out, "done: already has the drop-in") {
		t.Errorf("expected each host's state said, got %q", out)
	}
}

func TestHostTrimSetupWithoutABeadFilesNothingAndSaysHowToFileIt(t *testing.T) {
	log := bdRecording(t, `[]`)
	hostTrimStates(t, "laptop", map[string]string{"laptop": doctor.WSLTrimLacksDropIn})
	out, err := runMw(t, "host", "trim-setup")
	if err != nil {
		t.Fatalf("mw host trim-setup failed: %v\n%s", err, out)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Errorf("expected no bd call without --bead, got %s", calls)
	}
	if !strings.Contains(out, "laptop: WSL, lacks the drop-in") || !strings.Contains(out, "--bead") {
		t.Errorf("expected the lacking host named and --bead offered, got %q", out)
	}
}
