package contrib_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mw-gq6.1757: a fresh cloud box's bootstrap failed at its browsers step because
// Ubuntu's unattended-upgrades held the dpkg lock and the apt-get that
// `playwright install --with-deps` runs does not wait for it. Every apt run, that
// one too, reads /etc/apt/apt.conf.d, so a DPkg::Lock::Timeout written there
// before the first apt-get makes them all wait.

const aptLockWord = "DPkg::Lock::Timeout"

func bootstrapScriptLines(t *testing.T) []string {
	t.Helper()
	text, err := os.ReadFile("boost-bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(text), "\n")
}

func TestBootstrapWaitsForTheDpkgLockBeforeItsFirstAptGet(t *testing.T) {
	lock, apt := -1, -1
	for i, l := range bootstrapScriptLines(t) {
		code := strings.TrimSpace(l)
		if strings.HasPrefix(code, "#") {
			continue
		}
		if lock < 0 && strings.Contains(code, aptLockWord) {
			lock = i + 1
		}
		if apt < 0 && strings.Contains(code, "apt-get") {
			apt = i + 1
		}
	}
	if lock < 0 {
		t.Fatalf("boost-bootstrap.sh never sets %s: an apt run (apt-get, or the one playwright --with-deps makes) fails at once on a dpkg lock unattended-upgrades holds", aptLockWord)
	}
	if apt < 0 {
		t.Fatal("boost-bootstrap.sh runs no apt-get: this test's reading of it is out of date")
	}
	if lock > apt {
		t.Errorf("boost-bootstrap.sh sets %s at line %d, after its first apt-get at line %d", aptLockWord, lock, apt)
	}
}

func TestBootstrapDryRunNamesTheDpkgLockWaitBeforeAptGet(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash to run the bootstrap with")
	}
	script, err := filepath.Abs("boost-bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	home := filepath.Join(dir, "home")
	vault := filepath.Join(dir, "vault")
	for _, d := range []string{bin, home, vault} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a box with nothing installed, and a sudo that does nothing
	for name, body := range map[string]string{"dpkg-query": "exit 1\n", "sudo": "exit 1\n"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := command(t, bash, script, "--dry-run")
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "BOOST_VAULT=" + vault,
		"BOOST_HOST=cloud9", "BOOST_WG_ADDRESS=10.88.0.9", "BOOST_BEADS_HOST=10.88.0.1",
		"BOOST_WG_HUB_PUBKEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"BOOST_ROOT=" + filepath.Join(dir, "root"),
	}
	// the run stops at the vault step; the packages step is printed first
	out, _ := cmd.CombinedOutput()
	lock, apt := -1, -1
	for i, l := range strings.Split(string(out), "\n") {
		if !strings.Contains(l, "would:") {
			continue
		}
		if lock < 0 && strings.Contains(l, aptLockWord) {
			lock = i
		}
		if apt < 0 && strings.Contains(l, "apt-get") {
			apt = i
		}
	}
	if lock < 0 {
		t.Fatalf("the dry run names no step that sets %s:\n%s", aptLockWord, out)
	}
	if apt >= 0 && lock > apt {
		t.Errorf("the dry run names the lock wait after its first apt-get:\n%s", out)
	}
}
