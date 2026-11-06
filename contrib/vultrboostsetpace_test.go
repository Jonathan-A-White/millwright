package contrib_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boostPaceRig is boostAddressRig with a systemctl in front that notes its
// calls and, for `cat`, prints the shipped hourly timer and then the
// drop-in the box's HOME holds, as systemd does. It returns the environment,
// the file ssh notes its calls in, the systemctl calls' file and the box's
// drop-in path (the box is this host: the ssh stand-in runs the script here).
func boostPaceRig(t *testing.T) (env []string, sshCalls, ctlCalls, dropin string) {
	t.Helper()
	env, sshCalls = boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	ctlCalls = filepath.Join(home, "ctl-calls")
	dropin = filepath.Join(home, ".config", "systemd", "user", "mw-dispatch.timer.d", "boost-pace.conf")
	bin := filepath.Join(home, "ctl-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/bash\n" +
		"echo \"$*\" >>" + ctlCalls + "\n" +
		"if [ \"$2\" = cat ]; then printf '[Timer]\\nOnCalendar=hourly\\n'; [ -f " + dropin + " ] && { echo; echo '# drop-in'; cat " + dropin + "; }; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			env[i] = "PATH=" + bin + ":" + v
		}
	}
	return env, sshCalls, ctlCalls, dropin
}

func TestVultrBoostSetPaceWritesTheTimerDropInOnABoxOnTheHourlyTimer(t *testing.T) {
	env, _, ctlCalls, dropin := boostPaceRig(t)
	out, errOut, err := runBoost(t, env, "set-pace", "--name", "cloud1")
	if err != nil {
		t.Fatalf("set-pace: %v: %s", err, errOut)
	}
	if !strings.HasSuffix(out, "pace = *:0/5\n") {
		t.Errorf("expected the pace the timer ends with last, got %q", out)
	}
	if got, want := strings.TrimSpace(mustRead(t, dropin)), "[Timer]\nOnCalendar=\nOnCalendar=*:0/5"; got != want {
		t.Errorf("expected the drop-in boost-bootstrap.sh writes, got %q", got)
	}
	calls := mustRead(t, ctlCalls)
	for _, want := range []string{"--user daemon-reload", "--user restart mw-dispatch.timer", "--user start --no-block mw-dispatch.service"} {
		if !strings.Contains(calls, want) {
			t.Errorf("expected systemctl %q, got:\n%s", want, calls)
		}
	}
}

func TestVultrBoostSetPaceLeavesABoxAlreadyAtThePaceAlone(t *testing.T) {
	env, _, ctlCalls, dropin := boostPaceRig(t)
	if err := os.MkdirAll(filepath.Dir(dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	// The bootstrap's own file, with the newline `put` ends it with.
	if err := os.WriteFile(dropin, []byte("[Timer]\nOnCalendar=\nOnCalendar=*:0/5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "set-pace", "--name", "cloud1")
	if err != nil {
		t.Fatalf("set-pace: %v: %s", err, errOut)
	}
	if !strings.HasSuffix(out, "pace = *:0/5\n") {
		t.Errorf("expected the pace the timer ends with, got %q", out)
	}
	if calls := mustRead(t, ctlCalls); strings.Contains(calls, "restart") || strings.Contains(calls, "start ") {
		t.Errorf("expected the timer left running as it was, got systemctl calls:\n%s", calls)
	}
}

func TestVultrBoostSetPaceDryRunRunsNothing(t *testing.T) {
	env, sshCalls, _, _ := boostPaceRig(t)
	out, errOut, err := runBoost(t, env, "set-pace", "--dry-run", "--name", "cloud1")
	if err != nil {
		t.Fatalf("set-pace --dry-run: %v: %s", err, errOut)
	}
	if !strings.Contains(out, "set-pace --dry-run: nothing below is run") || !strings.Contains(out, "OnCalendar=*:0/5") {
		t.Errorf("expected the steps to be printed, got %q", out)
	}
	if _, err := os.Stat(sshCalls); err == nil {
		t.Errorf("--dry-run called ssh: %s", mustRead(t, sshCalls))
	}
}
