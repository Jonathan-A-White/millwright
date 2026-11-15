package contrib_test

// mw-gq6.1309: the CPUWeight drop-in for dolt-beads.service was a file in contrib/
// and a paragraph of steps, so a host that served the beads had it only when a
// person typed them. scripts/install-dolt-priority.sh puts it there and reads the
// weight back. This test runs it in a throwaway config home with a stand-in
// systemctl first on PATH: the stand-in reports the weight of the drop-in file it
// finds under that home (as systemd would after a reload), so a script that did not
// install the file cannot read back CPUWeight=1000. It never touches the real
// ~/.config or the real user manager.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeSystemctl = `#!/bin/sh
echo "systemctl $*" >> "$STAND_LOG"
case "$*" in
"--user show dolt-beads -p CPUWeight")
	f="$XDG_CONFIG_HOME/systemd/user/dolt-beads.service.d/priority.conf"
	if [ -f "$f" ] && [ -f "$STAND_RELOADED" ]; then
		sed -n 's/^CPUWeight=/CPUWeight=/p' "$f"
	else
		echo "CPUWeight=[not set]"
	fi
	;;
"--user daemon-reload") : > "$STAND_RELOADED" ;;
esac
exit 0
`

type doltRun struct {
	out    string
	err    error
	config string
	log    string
}

func runDoltInstall(t *testing.T, args ...string) doltRun {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(fakeSystemctl), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	log := filepath.Join(dir, "calls.log")
	cmd := exec.Command("sh", append([]string{"../scripts/install-dolt-priority.sh"}, args...)...)
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"HOME=" + dir,
		"XDG_CONFIG_HOME=" + config,
		"STAND_LOG=" + log,
		"STAND_RELOADED=" + filepath.Join(dir, "reloaded"),
	}
	out, err := cmd.CombinedOutput()
	return doltRun{out: string(out), err: err, config: config, log: log}
}

func (r doltRun) dropIn() string {
	return filepath.Join(r.config, "systemd", "user", "dolt-beads.service.d", "priority.conf")
}

func (r doltRun) calls() string {
	b, _ := os.ReadFile(r.log)
	return string(b)
}

func TestInstallDoltPriorityPutsTheDropInWhereSystemdReadsItAndReadsTheWeightBack(t *testing.T) {
	r := runDoltInstall(t)
	if r.err != nil {
		t.Fatalf("the installer failed: %v\n%s", r.err, r.out)
	}
	want, err := os.ReadFile("dolt-beads/priority.conf")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(r.dropIn())
	if err != nil {
		t.Fatalf("no drop-in under the user's systemd directory: %v\n%s", err, r.out)
	}
	if string(got) != string(want) {
		t.Errorf("the installed drop-in is not contrib/dolt-beads/priority.conf")
	}
	if !strings.Contains(r.out, "CPUWeight=1000") {
		t.Errorf("the installer did not print the weight it read back, CPUWeight=1000:\n%s", r.out)
	}
	calls := r.calls()
	reload := strings.Index(calls, "systemctl --user daemon-reload")
	restart := strings.Index(calls, "systemctl --user restart dolt-beads")
	show := strings.Index(calls, "systemctl --user show dolt-beads -p CPUWeight")
	if reload < 0 || restart < reload || show < restart {
		t.Errorf("want daemon-reload, then restart dolt-beads, then show CPUWeight; calls:\n%s", calls)
	}
}

func TestInstallDoltPriorityRunTwiceLeavesTheSameDropIn(t *testing.T) {
	r := runDoltInstall(t)
	if r.err != nil {
		t.Fatalf("first run failed: %v\n%s", r.err, r.out)
	}
	cmd := exec.Command("sh", "../scripts/install-dolt-priority.sh")
	cmd.Env = []string{"PATH=" + filepath.Dir(r.log) + "/bin:/usr/bin:/bin", "HOME=" + filepath.Dir(r.log),
		"XDG_CONFIG_HOME=" + r.config, "STAND_LOG=" + r.log, "STAND_RELOADED=" + filepath.Join(filepath.Dir(r.log), "reloaded")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("second run failed: %v\n%s", err, out)
	}
	want, _ := os.ReadFile("dolt-beads/priority.conf")
	got, _ := os.ReadFile(r.dropIn())
	if string(got) != string(want) {
		t.Error("the drop-in differs after a second run")
	}
}

func TestInstallDoltPriorityDryRunChangesNothing(t *testing.T) {
	r := runDoltInstall(t, "--dry-run")
	if r.err != nil {
		t.Fatalf("--dry-run failed: %v\n%s", r.err, r.out)
	}
	if _, err := os.Stat(r.dropIn()); err == nil {
		t.Error("--dry-run wrote the drop-in")
	}
	if strings.Contains(r.calls(), "daemon-reload") || strings.Contains(r.calls(), "restart") {
		t.Errorf("--dry-run ran systemctl:\n%s", r.calls())
	}
	if !strings.Contains(r.out, "priority.conf") {
		t.Errorf("--dry-run did not say what it would do:\n%s", r.out)
	}
}

func TestInstallDoltPriorityFailsWhenTheWeightDoesNotReadBack(t *testing.T) {
	// A user manager that does not take the weight (the cpu controller not
	// delegated) reads back something else: the installer must say so and exit non-zero.
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	_ = os.MkdirAll(bin, 0o755)
	stub := "#!/bin/sh\ncase \"$*\" in *show*) echo 'CPUWeight=[not set]';; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "../scripts/install-dolt-priority.sh")
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + dir, "XDG_CONFIG_HOME=" + filepath.Join(dir, "c")}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the installer exited 0 though CPUWeight read back unset:\n%s", out)
	}
	if !strings.Contains(string(out), "CPUWeight") {
		t.Errorf("the failure does not name CPUWeight:\n%s", out)
	}
}
