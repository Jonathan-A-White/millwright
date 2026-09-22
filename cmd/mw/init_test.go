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

// The real thing, end to end, for a second host joining a vault that already
// exists: real git and a real bd, in a throwaway HOME. The vault this host
// joins is set up the way the designated migrator does it — mw init, then bd
// sync --yes once to adopt a bare git remote as the Dolt remote — which mw
// init --join itself must never do.
func TestInitJoinBringsThisHostOntoAnExistingVaultWithoutEverMigratingIt(t *testing.T) {
	for _, program := range []string{beads.Program, "git"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not on PATH", program)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("MW_VAULT", "")

	// The designated migrator's own machine, entirely separate from the
	// joiner's: its mw init must not be what writes the joiner's config.toml.
	originHome := t.TempDir()
	origin := filepath.Join(originHome, "origin")
	bare := filepath.Join(originHome, "bare.git")

	t.Setenv("HOME", originHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(originHome, ".config"))
	mustRun(t, originHome, "git", "init", "-q", "--bare", "-b", "main", bare)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"init", "--vault", origin, "--prefix", "tst", "--host", "origin"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw init: %v\n%s", err, out)
	}

	// The designated migrator's own one-time steps: a remote, a push, and the
	// explicit consent bd asks for before it will adopt a git remote as its
	// Dolt one. mw init --join must never take any of these.
	mustRun(t, origin, "git", "remote", "add", "origin", bare)
	mustRun(t, origin, "git", "push", "-q", "-u", "origin", "main")
	bestEffortRun(t, origin, beads.Program, "sync", "--yes")
	mustRun(t, origin, "git", "add", "-A")
	mustRun(t, origin, "git", "-c", "user.name=origin", "-c", "user.email=origin@test.example",
		"commit", "-q", "-m", "bd sync: adopt the dolt remote")
	mustRun(t, origin, beads.Program, "dolt", "push")
	mustRun(t, origin, beads.Program, "sync")
	mustRun(t, origin, "git", "push", "-q", "origin", "main")

	// The joiner's own machine: a throwaway HOME of its own, with no config
	// file yet for mw init --join to write.
	home := t.TempDir()
	joined := filepath.Join(home, "j")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	out.Reset()
	root = newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"init", "--join", "file://" + bare, "--vault", joined, "--host", "joiner"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw init --join: %v\n%s", err, out)
	}
	if said := strings.ToLower(out.String()); strings.Contains(said, "bd init") {
		t.Errorf("mw init --join mentioned bd init:\n%s", out)
	}

	mustRun(t, joined, beads.Program, "list")

	if _, err := os.Stat(filepath.Join(joined, "seats", "mayor", "charter.md")); err != nil {
		t.Errorf("no Mayor's charter in the joined vault: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".config", "mw", "config.toml")); err != nil ||
		!strings.Contains(string(got), `vault = "`+joined+`"`) || !strings.Contains(string(got), `host  = "joiner"`) {
		t.Errorf("config.toml holds %q (%v)", got, err)
	}

	// --join and --prefix together are refused, before anything is touched.
	elsewhere := filepath.Join(home, "not-made")
	out.Reset()
	root = newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"init", "--join", "file://" + bare, "--prefix", "tst", "--vault", elsewhere})
	if err := root.Execute(); err == nil {
		t.Error("mw init --join --prefix together succeeded")
	}
	if _, err := os.Stat(elsewhere); !os.IsNotExist(err) {
		t.Errorf("mw init --join --prefix wrote %s anyway", elsewhere)
	}
}

// mustRun runs one command in dir and fails the test if it does not exit
// clean.
func mustRun(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v: %s", program, args, dir, err, out)
	}
	return string(out)
}

// bestEffortRun runs one command in dir without failing the test: a setup
// step here is expected to fail on its first try, before the remote it is
// adopting has anything in it to pull.
func bestEffortRun(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	return string(out)
}
