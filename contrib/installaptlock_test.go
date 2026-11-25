package contrib_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mw-gq6.1758: scripts/install.sh ran apt-get update and apt-get install with
// no wait for the dpkg lock, so on a fresh Ubuntu box whose unattended-upgrades
// held it, step 1 failed at once (mw-gq6.1757 was the same fault in
// contrib/boost-bootstrap.sh). A host with no apt.conf.d file that sets
// DPkg::Lock::Timeout gets it on the command line instead.

// installAptCalls runs scripts/install.sh as root on a stand-in Ubuntu box with
// nothing installed and returns each line the stand-in apt-get recorded. confDir
// holds the files the script reads as /etc/apt/apt.conf.d; put files there first.
func installAptCalls(t *testing.T, confDir string) []string {
	t.Helper()
	_, calls := installRun(t, confDir, "0")
	if calls == nil {
		t.Fatal("install.sh ran no apt-get")
	}
	return calls
}

// installRun runs scripts/install.sh as the user whose id the stand-in id prints
// ("0" is root) and returns its combined output and each line the stand-in
// apt-get recorded (nil when it ran none).
func installRun(t *testing.T, confDir, uid string) (string, []string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to run install.sh with")
	}
	script, err := filepath.Abs("../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	home := filepath.Join(dir, "home")
	for _, d := range []string{bin, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// PATH is the stand-ins and the plain tools the script reads with, so none of
	// its apt packages is found and every one is missing.
	for _, tool := range []string{"sed", "head", "tr", "uname", "dirname", "grep", "ls", "cat", "awk", "mktemp", "rm", "cut", "sha256sum"} {
		if p, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	calls := filepath.Join(dir, "calls")
	stands := map[string]string{
		"id":      "echo " + uid + "\n",
		"apt-get": `echo "$*" >>"` + calls + "\"\n",
		// the run is over once step 1 is: the Go download is refused
		"curl": "exit 1\n",
		"git":  "exit 1\n",
	}
	for name, body := range stands {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	osRelease := filepath.Join(dir, "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=ubuntu\nID_LIKE=debian\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := command(t, sh, script)
	cmd.Env = []string{
		"PATH=" + bin, "HOME=" + home, "MW_HOME=" + filepath.Join(dir, "rig"),
		"MW_BIN=" + filepath.Join(dir, "mwbin"), "MW_GO_ROOT=" + filepath.Join(dir, "go"),
		"MW_INSTALL_OS_RELEASE=" + osRelease, "MW_INSTALL_APT_CONF_DIR=" + confDir,
	}
	out, _ := cmd.CombinedOutput() // it stops at the Go step; step 1 is what is read
	text, err := os.ReadFile(calls)
	if err != nil {
		return string(out), nil
	}
	return string(out), strings.Split(strings.TrimSpace(string(text)), "\n")
}

func TestInstallWaitsForTheDpkgLockOnAHostThatDoesNotSetIt(t *testing.T) {
	calls := installAptCalls(t, t.TempDir())
	if len(calls) != 2 {
		t.Fatalf("want apt-get update and apt-get install, got %q", calls)
	}
	for _, c := range calls {
		if !strings.Contains(c, aptLockWord) {
			t.Errorf("apt-get %q does not pass %s: it fails at once on a dpkg lock unattended-upgrades holds", c, aptLockWord)
		}
	}
	if !strings.HasPrefix(calls[0], "-o ") || !strings.Contains(calls[0], "update") {
		t.Errorf("the first call is not an update with the wait: %q", calls[0])
	}
	if !strings.Contains(calls[1], "install -y --no-install-recommends") {
		t.Errorf("the second call is not the install: %q", calls[1])
	}
}

func TestInstallLeavesAptAloneOnAHostThatSetsTheWaitInAptConf(t *testing.T) {
	conf := t.TempDir()
	if err := os.WriteFile(filepath.Join(conf, "90boost-dpkg-lock-wait"), []byte(aptLockWord+" \"600\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range installAptCalls(t, conf) {
		if strings.Contains(c, aptLockWord) {
			t.Errorf("apt-get %q repeats a wait apt.conf.d already sets", c)
		}
	}
}

// mw-gq6.1759: a user who is not root is told the one apt-get install command to
// run by hand; run on a fresh box it failed on the dpkg lock as the script's own
// run did before mw-gq6.1758, because the printed command had no wait.
func TestInstallPrintedCommandWaits(t *testing.T) {
	out, calls := installRun(t, t.TempDir(), "1000")
	if calls != nil {
		t.Fatalf("a user who is not root ran apt-get: %q", calls)
	}
	var printed string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "apt-get") && strings.Contains(l, "install") && strings.HasPrefix(strings.TrimSpace(l), "apt-get") {
			printed = l
		}
	}
	if printed == "" {
		t.Fatalf("install.sh printed no apt-get install command:\n%s", out)
	}
	if !strings.Contains(printed, aptLockWord) {
		t.Errorf("the command printed for a user to run by hand does not pass %s: %q", aptLockWord, printed)
	}
}

func TestInstallPrintedCommandLeavesAptAloneOnAHostThatSetsTheWaitInAptConf(t *testing.T) {
	conf := t.TempDir()
	if err := os.WriteFile(filepath.Join(conf, "90boost-dpkg-lock-wait"), []byte(aptLockWord+" \"600\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := installRun(t, conf, "1000")
	if strings.Contains(out, aptLockWord) {
		t.Errorf("the printed command repeats a wait apt.conf.d already sets:\n%s", out)
	}
}
