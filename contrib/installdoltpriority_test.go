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
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const fakeSystemctl = `#!/bin/sh
echo "systemctl $*" >> "$STAND_LOG"
case "$*" in
"--user show "*" -p CPUWeight")
	f="$XDG_CONFIG_HOME/systemd/user/$3.service.d/priority.conf"
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

func (r doltRun) dropIn() string { return r.dropInOf("dolt-beads") }

func (r doltRun) dropInOf(unit string) string {
	return filepath.Join(r.config, "systemd", "user", unit+".service.d", "priority.conf")
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

// mw-gq6.1571: the Laptop's postern-backend starved while Builders' builds held
// the load at 25 to 30 of 20 cores, and the Governor's phone fell to "Live from
// the chain". The same installer takes the unit's name and puts the same kind of
// drop-in under it.
func TestInstallPriorityTakesPosternBackendAndReadsItsWeightBack(t *testing.T) {
	r := runDoltInstall(t, "postern-backend")
	if r.err != nil {
		t.Fatalf("the installer failed: %v\n%s", r.err, r.out)
	}
	want, err := os.ReadFile("postern-backend/priority.conf")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(r.dropInOf("postern-backend"))
	if err != nil {
		t.Fatalf("no postern-backend drop-in under the user's systemd directory: %v\n%s", err, r.out)
	}
	if string(got) != string(want) {
		t.Errorf("the installed drop-in is not contrib/postern-backend/priority.conf")
	}
	if _, err := os.Stat(r.dropIn()); err == nil {
		t.Error("naming postern-backend also installed the dolt-beads drop-in")
	}
	if !strings.Contains(r.out, "CPUWeight=1000") {
		t.Errorf("the installer did not print the weight it read back:\n%s", r.out)
	}
	calls := r.calls()
	if strings.Contains(calls, "restart dolt-beads") || !strings.Contains(calls, "systemctl --user restart postern-backend") {
		t.Errorf("want only postern-backend restarted; calls:\n%s", calls)
	}
}

func TestInstallPriorityRefusesAUnitItHasNoDropInFor(t *testing.T) {
	r := runDoltInstall(t, "mw-dispatch")
	if r.err == nil {
		t.Fatalf("the installer took a unit with no drop-in:\n%s", r.out)
	}
	if r.calls() != "" {
		t.Errorf("a refused unit ran systemctl:\n%s", r.calls())
	}
}

func TestInstallPriorityDryRunNamesThePosternDropIn(t *testing.T) {
	r := runDoltInstall(t, "--dry-run", "postern-backend")
	if r.err != nil {
		t.Fatalf("--dry-run failed: %v\n%s", r.err, r.out)
	}
	if !strings.Contains(r.out, "postern-backend/priority.conf") || !strings.Contains(r.out, "postern-backend.service.d") {
		t.Errorf("--dry-run did not name the postern-backend drop-in:\n%s", r.out)
	}
	if _, err := os.Stat(r.dropInOf("postern-backend")); err == nil {
		t.Error("--dry-run wrote the drop-in")
	}
}

// The factory services the Governor waits on get the CPU before the test runners
// do. dolt-beads has its drop-in; the postern backend gets one, and the view
// follower, which the tap shows through within two seconds, no longer steps aside
// at Nice=19 beside builds.
func TestThePosternBackendDropInGivesItTheCPUBeforeTheTestRunners(t *testing.T) {
	text, err := os.ReadFile("postern-backend/priority.conf")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CPUWeight=(\d+)$`).FindSubmatch(text)
	if m == nil {
		t.Fatal("postern-backend/priority.conf has no CPUWeight=<n> line")
	}
	if n, _ := strconv.Atoi(string(m[1])); n < 500 || n > 10000 {
		t.Errorf("CPUWeight=%d: expected well above the default 100, at most 10000", n)
	}
	if !regexp.MustCompile(`(?m)^\[Service\]$`).Match(text) {
		t.Error("the drop-in has no [Service] section, so systemd would ignore CPUWeight")
	}
	if regexp.MustCompile(`(?m)^Nice=-`).Match(text) {
		t.Error("a user unit cannot lower its niceness without root: use CPUWeight")
	}
}

func TestTheViewFollowerDoesNotStepAsideBesideBuilds(t *testing.T) {
	text, err := os.ReadFile("systemd/mw-view-follow.service")
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?m)^Nice=`).Match(text) {
		t.Error("mw-view-follow.service sets Nice=: under a Builder's build load the Governor's tap waits on it")
	}
	m := regexp.MustCompile(`(?m)^CPUWeight=(\d+)$`).FindSubmatch(text)
	if m == nil {
		t.Fatal("mw-view-follow.service has no CPUWeight=<n> line")
	}
	if n, _ := strconv.Atoi(string(m[1])); n < 500 || n > 10000 {
		t.Errorf("CPUWeight=%d: expected well above the default 100, at most 10000", n)
	}
}
