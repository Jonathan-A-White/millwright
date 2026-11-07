package rig_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hostileChildEnv marks the run of this test binary that the test below makes
// under a hostile HOME, so that run does not make another.
const hostileChildEnv = "MW_TEST_HOSTILE_HOME_CHILD"

// hostileGitConfig is a host's git config out to change a test's verdict: a
// rewrite of every ssh github url, an identity of its own, a hooks path whose
// pre-commit always refuses, signing through a program that always fails, and
// a default branch that is not main (mw-gq6.1027, after mw-gq6.1017).
func hostileGitConfig(hooks string) string {
	return "[user]\n\tname = Hostile Host\n\temail = hostile@host.invalid\n" +
		"[core]\n\thooksPath = " + hooks + "\n" +
		"[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = /bin/false\n" +
		"[init]\n\tdefaultBranch = trunk\n" +
		"[url \"https://hostile.invalid/\"]\n\tinsteadOf = git@github.com:\n"
}

// TestThePackagesSuitePassesUnderAHostileHostGitConfig runs this package's own
// test binary again, whole, with HOME and XDG_CONFIG_HOME at a directory whose
// git config is hostile and with no GIT_CONFIG_GLOBAL or GIT_CONFIG_NOSYSTEM
// set: what a host with an insteadOf, an identity, a hooks path or a signing
// key gives the suite. Every git the suite runs, its fixtures and the adapters
// under test alike, must keep that config out.
func TestThePackagesSuitePassesUnderAHostileHostGitConfig(t *testing.T) {
	if os.Getenv(hostileChildEnv) != "" {
		t.Skip("this is the run under the hostile HOME")
	}
	home := t.TempDir()
	hooks := filepath.Join(home, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho hostile hook >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := hostileGitConfig(hooks)
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	env := []string{}
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "GIT_CONFIG_"), strings.HasPrefix(kv, "HOME="), strings.HasPrefix(kv, "XDG_CONFIG_HOME="):
		default:
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), hostileChildEnv+"=1")

	cmd := exec.Command(os.Args[0], "-test.timeout=8m")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the suite fails under a hostile host git config: %v\n%s", err, tail(string(out), 6000))
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
