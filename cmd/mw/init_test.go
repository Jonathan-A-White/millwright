//go:build beads_integration

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// The real thing, end to end: mw init through its own command tree, with real
// git and a real bd, in a throwaway HOME. Nothing here reaches the real
// ~/.config/mw or the factory's vault.
func TestInitMakesAVaultThatBeadsAndGitAreBothHappyWith(t *testing.T) {
	for _, program := range []string{beads.Program, "git"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not on PATH", program)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("MW_VAULT", "")
	vaultDir := filepath.Join(home, "v")

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"init", "--vault", vaultDir, "--prefix", "tst", "--host", "testhost", "--rig", "millwright=/work/mill"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw init: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(vaultDir, "seats", "mayor", "charter.md")); err != nil {
		t.Errorf("no Mayor's charter in the new vault: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".config", "mw", "config.toml")); err != nil ||
		!strings.Contains(string(got), `vault = "`+vaultDir+`"`) || !strings.Contains(string(got), `millwright = "/work/mill"`) {
		t.Errorf("config.toml holds %q (%v)", got, err)
	}

	inVault := func(program string, args ...string) string {
		cmd := exec.Command(program, args...)
		cmd.Dir = vaultDir
		got, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v in the new vault: %v: %s", program, args, err, got)
		}
		return string(got)
	}
	inVault(beads.Program, "list")
	if count := strings.TrimSpace(inVault("git", "rev-list", "--count", "HEAD")); count != "1" {
		t.Errorf("the vault has %s commits, want 1", count)
	}
	if left := strings.TrimSpace(inVault("git", "status", "--porcelain")); left != "" {
		t.Errorf("the first commit left this out:\n%s", left)
	}
	if tracked := inVault("git", "ls-files", ".beads/config.yaml"); !strings.Contains(tracked, ".beads/config.yaml") {
		t.Error("the beads config is not in the first commit, so another host could not bootstrap from it")
	}
	if trailers := strings.ToLower(inVault("git", "log", "--format=%B")); strings.Contains(trailers, "co-authored") {
		t.Errorf("the first commit carries a Co-Authored-By: %s", trailers)
	}

	// A second run in the same place is refused, and changes nothing.
	before := inVault("git", "rev-parse", "HEAD")
	root = newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"init", "--vault", vaultDir, "--prefix", "tst"})
	if err := root.Execute(); err == nil {
		t.Error("a second mw init in the same directory succeeded")
	}
	if after := inVault("git", "rev-parse", "HEAD"); after != before {
		t.Errorf("a refused mw init moved HEAD from %s to %s", before, after)
	}
}
