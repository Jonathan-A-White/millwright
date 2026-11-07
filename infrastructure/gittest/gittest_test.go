package gittest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"
)

// hostileHome makes a HOME whose git config is out to change a test's verdict:
// a rewrite of every ssh github url, an identity of its own, a hooks path whose
// pre-commit always refuses, and signing through a program that always fails.
func hostileHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	hooks := filepath.Join(home, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho hostile hook >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[user]\n\tname = Hostile Host\n\temail = hostile@host.invalid\n" +
		"[core]\n\thooksPath = " + hooks + "\n" +
		"[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = /bin/false\n" +
		"[init]\n\tdefaultBranch = trunk\n" +
		"[url \"https://hostile.invalid/\"]\n\tinsteadOf = git@github.com:\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func underHostileHome(t *testing.T) {
	t.Helper()
	home := hostileHome(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "")
	os.Unsetenv("GIT_CONFIG_GLOBAL")
	os.Unsetenv("GIT_CONFIG_NOSYSTEM")
}

func gitOK(t *testing.T, cmd *exec.Cmd, dir string) string {
	t.Helper()
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v in %s: %v: %s", cmd.Args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Without the helper, the hostile HOME is what git reads: this is the premise
// of the tests below, so they cannot pass for want of a hostile host.
func TestAHostileHomeChangesWhatPlainGitReads(t *testing.T) {
	underHostileHome(t)
	dir := t.TempDir()
	gitOK(t, exec.Command("git", "init", "-q"), dir)
	gitOK(t, exec.Command("git", "remote", "add", "origin", "git@github.com:o/r.git"), dir)
	if got := gitOK(t, exec.Command("git", "ls-remote", "--get-url", "origin"), dir); got != "https://hostile.invalid/o/r.git" {
		t.Fatalf("the hostile HOME should rewrite the url, git read %q", got)
	}
}

func TestCommandKeepsAHostileHomesGitConfigOut(t *testing.T) {
	underHostileHome(t)
	dir := t.TempDir()
	gitOK(t, gittest.Command("git", "init", "-q", "-b", "main"), dir)
	gitOK(t, gittest.Command("git", "remote", "add", "origin", "git@github.com:o/r.git"), dir)
	if got := gitOK(t, gittest.Command("git", "ls-remote", "--get-url", "origin"), dir); got != "git@github.com:o/r.git" {
		t.Errorf("the url was rewritten by the host's insteadOf: %q", got)
	}
	// The hostile pre-commit hook and the failing signer would refuse this.
	gitOK(t, gittest.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "one"), dir)
	if got := gitOK(t, gittest.Command("git", "log", "-1", "--format=%an"), dir); got != "t" {
		t.Errorf("the commit was authored as %q, not by the test's own identity", got)
	}
	if got := gitOK(t, gittest.Command("git", "symbolic-ref", "--short", "HEAD"), dir); got != "main" {
		t.Errorf("the branch is %q", got)
	}
}

func TestIsolateKeepsAHostileHomeOutOfEveryGitTheProcessRuns(t *testing.T) {
	underHostileHome(t)
	gittest.Isolate()
	dir := t.TempDir()
	gitOK(t, exec.Command("git", "init", "-q", "-b", "main"), dir)
	gitOK(t, exec.Command("git", "remote", "add", "origin", "git@github.com:o/r.git"), dir)
	if got := gitOK(t, exec.Command("git", "ls-remote", "--get-url", "origin"), dir); got != "git@github.com:o/r.git" {
		t.Errorf("the url was rewritten by the host's insteadOf: %q", got)
	}
	gitOK(t, exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "one"), dir)
}

func TestEnvSetsBothSettingsAndKeepsTheRest(t *testing.T) {
	t.Setenv("MW_GITTEST_KEEP", "yes")
	env := strings.Join(gittest.Env(), "\n")
	for _, want := range []string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "MW_GITTEST_KEEP=yes"} {
		if !strings.Contains(env, want) {
			t.Errorf("Env lacks %q", want)
		}
	}
}
