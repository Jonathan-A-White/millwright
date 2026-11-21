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

// --check lists, for each WSL host without the drop-in, a pending trim step,
// and fails so a script sees it; a host with the drop-in or off WSL is said
// and not listed. It files nothing and prints no PowerShell line.
func TestHostTrimSetupCheckNamesEachWSLHostLackingTheDropIn(t *testing.T) {
	log := bdRecording(t, `[]`)
	hostTrimStates(t, "laptop", map[string]string{
		"laptop":  doctor.WSLTrimLacksDropIn,
		"desktop": doctor.WSLTrimLacksDropIn,
		"done":    doctor.WSLTrimHasDropIn,
		"vps":     doctor.WSLTrimNotWSL,
	})
	out, err := runMw(t, "host", "trim-setup", "--check")
	if err == nil {
		t.Errorf("expected a failure while a host lacks the drop-in, got %q", out)
	}
	for _, host := range []string{"laptop", "desktop"} {
		if !strings.Contains(out, "pending trim step: "+host+"\n") {
			t.Errorf("expected %s listed as pending, got %q", host, out)
		}
	}
	for _, host := range []string{"done", "vps"} {
		if strings.Contains(out, "pending trim step: "+host) {
			t.Errorf("expected %s not listed, got %q", host, out)
		}
	}
	if strings.Contains(out, "wsl -d ") {
		t.Errorf("expected no PowerShell line with --check, got %q", out)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Errorf("expected no bd call with --check, got %s", calls)
	}
}

func TestHostTrimSetupCheckPassesWhenEveryWSLHostHasTheDropIn(t *testing.T) {
	hostTrimStates(t, "laptop", map[string]string{"laptop": doctor.WSLTrimHasDropIn, "vps": doctor.WSLTrimNotWSL})
	out, err := runMw(t, "host", "trim-setup", "--check")
	if err != nil || strings.Contains(out, "pending trim step") {
		t.Fatalf("expected a pass with nothing pending, got %v %q", err, out)
	}
}

// pasteBlocks are the fenced blocks of what a command printed, each as the
// lines between its fences.
func pasteBlocks(out string) [][]string {
	var blocks [][]string
	var current []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "```") {
			if in {
				blocks = append(blocks, current)
				current = nil
			}
			in = !in
			continue
		}
		if in {
			current = append(current, line)
		}
	}
	return blocks
}

// mw-gq6.1381: the Governor copies the whole block, so the drop-in is ONE line
// alone in a fenced block, with no prompt and no line break inside it.
func TestHostTrimSetupPrintsTheDropInAsOneLineAloneInAFencedBlock(t *testing.T) {
	hostTrimStates(t, "laptop", map[string]string{"laptop": doctor.WSLTrimHasDropIn})
	out, err := runMw(t, "host", "trim-setup")
	if err != nil {
		t.Fatalf("mw host trim-setup failed: %v\n%s", err, out)
	}
	blocks := pasteBlocks(out)
	if len(blocks) != 1 {
		t.Fatalf("want one fenced block to copy, got %d in:\n%s", len(blocks), out)
	}
	if len(blocks[0]) != 1 || blocks[0][0] != doctor.WSLTrimPowerShellLine() {
		t.Errorf("the block should hold the PowerShell line alone, got %q", blocks[0])
	}
	if strings.Contains(doctor.WSLTrimPowerShellLine(), "\n") || strings.HasPrefix(blocks[0][0], "PS ") || strings.HasPrefix(blocks[0][0], ">") {
		t.Errorf("the line holds a line break or a prompt: %q", blocks[0][0])
	}
}

// The sweep of the other `mw host` subcommands that print a Windows paste:
// wsl-flag --install-line prints each paste as one line alone in a fenced block;
// giveway's pastes are a script of several lines by design (each begins with a
// comment saying what it does), so they are held to no prompt on any line.
func TestTheOtherHostPastesCarryNoPromptAndWslFlagsIsOneLine(t *testing.T) {
	giveWayPinned(t)
	out, err := runMw(t, "host", "wsl-flag", "--install-line")
	if err != nil {
		t.Fatalf("mw host wsl-flag --install-line failed: %v\n%s", err, out)
	}
	blocks := pasteBlocks(out)
	if len(blocks) != 2 {
		t.Fatalf("want the install and the way back as two fenced blocks, got %d:\n%.300s", len(blocks), out)
	}
	for _, block := range blocks {
		if len(block) != 1 || strings.HasPrefix(block[0], "PS ") || strings.HasPrefix(block[0], ">") {
			t.Errorf("a wsl-flag paste must be one line with no prompt, got %d lines starting %.40q", len(block), block)
		}
	}
	for _, args := range [][]string{
		{"host", "giveway", "--install-line"},
		{"host", "giveway", "--uninstall-line"},
		{"host", "giveway", "--mode-line", "freeze"},
	} {
		out, err := runMw(t, args...)
		if err != nil {
			t.Fatalf("mw %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "PS ") || strings.HasPrefix(line, "> ") || strings.Contains(line, "```") {
				t.Errorf("mw %s: a line reads as a prompt or a fence: %.60q", strings.Join(args, " "), line)
			}
		}
	}
}

// mw-6ww.185.17: a host whose timer drop-in is the old weekly one is not done:
// --bead files its step, --check lists it as pending, and each says why.
func TestHostTrimSetupTreatsTheOldWeeklyDropInAsNotDone(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	fakeHandsPush(t, nil)
	log := bdRecording(t, `[{"id": "mw-gq6.1020", "title": "Trim", "status": "open", "issue_type": "task"}]`)
	hostTrimStates(t, "laptop", map[string]string{"laptop": doctor.WSLTrimOldDropIn, "done": doctor.WSLTrimHasDropIn})

	out, err := runMw(t, "host", "trim-setup", "--check")
	if err == nil || !strings.Contains(out, "pending trim step: laptop\n") || strings.Contains(out, "pending trim step: done") {
		t.Errorf("expected --check to fail listing laptop only, got %v %q", err, out)
	}

	out, err = runMw(t, "host", "trim-setup", "--bead", "mw-gq6.1020")
	if err != nil {
		t.Fatalf("mw host trim-setup --bead failed: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "HANDS STEP wsl-fstrim-laptop on laptop as root:") {
		t.Errorf("expected a step for the host with the old drop-in, got:\n%s", calls)
	}
	if !strings.Contains(out, "laptop: WSL, has the old weekly drop-in") || !strings.Contains(out, doctor.WSLTrimPowerShellLine()) {
		t.Errorf("expected the old drop-in said and the root line printed, got %q", out)
	}
}
