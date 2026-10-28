package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mw member serve --help documents --way-back and the hand step.
func TestMemberServeHelpDocumentsWayBackAndTheHandStep(t *testing.T) {
	root := newRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"member", "serve", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw member serve --help: %v", err)
	}
	for _, want := range []string{"--way-back", "loginctl enable-linger", "mw-dispatch.timer", "BY HAND", "codex"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--help does not mention %q", want)
		}
	}
}

// The real mw member serve, under a throwaway HOME with stand-in systemctl and
// loginctl and tools, writes units that scripts/check-timer-units.sh accepts.
func TestMemberServeWritesUnitsTheTimerUnitsCheckAccepts(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	for _, tool := range []string{"mw", "bd", "git", "tmux", "codex"} {
		writeScript(t, bin, tool, "exit 0")
	}
	calls := filepath.Join(home, "systemctl.log")
	writeScript(t, bin, "systemctl", `echo "$@" >> `+calls)
	writeScript(t, bin, "loginctl", `echo Linger=yes`)
	hostPath := os.Getenv("PATH")
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+hostPath)
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte("cap = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"member", "serve"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw member serve: %v\n%s", err, out)
	}
	env, err := os.ReadFile(filepath.Join(home, ".config", "mw", "dispatch.env"))
	if err != nil || !strings.HasPrefix(string(env[strings.Index(string(env), "PATH="):]), "PATH="+bin) {
		t.Fatalf("dispatch.env = %q, %v; want a PATH line naming %s", env, err, bin)
	}
	if got, _ := os.ReadFile(calls); !strings.Contains(string(got), "--user enable --now mw-dispatch.timer") {
		t.Errorf("systemctl ran:\n%s\nwant the timer enabled", got)
	}

	script, err := filepath.Abs("../../scripts/check-timer-units.sh")
	if err != nil {
		t.Fatal(err)
	}
	runCheck := func() (string, error) {
		check := exec.Command("sh", script)
		// The check runs with the host's own git and systemd-analyze, not the stand-ins;
		// HOME is the throwaway one, so no unit of the host's is read.
		check.Env = append(os.Environ(), "PATH="+hostPath, "MW_MEMBER_UNITS="+filepath.Join(home, ".config", "systemd", "user"))
		said, err := check.CombinedOutput()
		return string(said), err
	}
	if said, err := runCheck(); err != nil {
		t.Fatalf("check-timer-units.sh refused a member's units: %v\n%s", err, said)
	}

	// A member's unit that is not the holder's is named.
	service := filepath.Join(home, ".config", "systemd", "user", "mw-dispatch.service")
	if err := os.WriteFile(service, []byte("[Service]\nExecStart=/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	said, err := runCheck()
	if err == nil || !strings.Contains(said, "is not contrib/systemd/mw-dispatch.service") {
		t.Fatalf("check-timer-units.sh passed a member's service that is not the holder's: %v\n%s", err, said)
	}
}

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
