package rig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A host whose home holds a global git config with a url insteadOf rewrite
// (the Laptop's ssh to https one) must not change what a git in this package's
// tests reads back: TestMain gives the binary a git config of its own
// (mw-gq6.1033).
func TestRigTestsReadAnSSHOriginBackAsStoredWhateverTheHostsHomeRewrites(t *testing.T) {
	home := t.TempDir()
	hostConfig := "[url \"https://github.com/\"]\n\tinsteadOf = git@github.com:\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(hostConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	dir := t.TempDir()
	const stored = "git@github.com:o/rig.git"
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"remote", "add", "origin", stored},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != stored {
		t.Fatalf("git read the origin back as %q, want the stored %q: the host's git config is leaking into the rig tests", got, stored)
	}
}
