package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexMemberHome is a member's home as mw member init --harness codex leaves
// it, less the Dolt unit: a config file naming the vault and the codex harness,
// and a vault with a Mayor charter, a handoff and an AGENTS.md. A fake codex
// is first on the PATH; it records its arguments and its directory and ends.
func codexMemberHome(t *testing.T) (vault, seen string) {
	t.Helper()
	home := t.TempDir()
	vault = filepath.Join(home, "luke-vault")
	for dir, files := range map[string]map[string]string{
		filepath.Join(vault, "seats", "mayor"):             {"charter.md": "# Mayor\n"},
		filepath.Join(vault, "seats", "mayor", "handoffs"): {"2026-10-28-00.md": "# Handoff\n"},
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, text := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(vault, "AGENTS.md"), []byte("Boot from `seats/mayor/charter.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	seen = filepath.Join(home, "codex.seen")
	fake := "#!/bin/sh\npwd > " + seen + ".tmp\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> " + seen + ".tmp; done\nmv " + seen + ".tmp " + seen + "\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	config := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("vault = %q\nhost  = \"laptop\"\ncap   = 1\n\n[next]\nharness = \"codex\"\n\n[rigs]\n", vault)
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, name := range []string{"MW_VAULT", "MW_HOST", "MW_CAP"} {
		t.Setenv(name, "")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Setenv(TmuxSocketEnv, socket)
	t.Cleanup(func() { exec.Command("tmux", "-L", socket, "kill-server").Run() })
	return vault, seen
}

func seatUp(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"seat", "up", "mayor"}, args...))
	err := root.Execute()
	return out.String(), err
}

func theFakeCodexSaw(t *testing.T, seen string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(seen); err == nil {
			return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the fake codex was never run: no %s", seen)
	return nil
}

func TestSeatUpOnACodexHostSpawnsCodexInTheVault(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux to open a window in")
	}
	vault, seen := codexMemberHome(t)
	out, err := seatUp(t)
	if err != nil {
		t.Fatalf("mw seat up mayor: %v\n%s", err, out)
	}

	got := theFakeCodexSaw(t, seen)
	want := []string{
		"-m", "gpt-6.1-sol", "-c", "model_reasoning_effort=high",
		"--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access=true",
		"-a", "never",
	}
	if got[0] != vault {
		t.Errorf("expected codex to start in the vault %s, where AGENTS.md is, got %s", vault, got[0])
	}
	if args := got[1 : len(got)-1]; strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("codex was started with\n%q\nwant\n%q then the kickoff", args, want)
	}
	if kickoff := got[len(got)-1]; !strings.Contains(kickoff, "seats/mayor/handoffs/2026-10-28-00.md") {
		t.Errorf("expected the kickoff to name the newest handoff, got %q", kickoff)
	}
}

func TestSeatUpAttendedOnACodexHostAsksOnRequest(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux to open a window in")
	}
	_, seen := codexMemberHome(t)
	if out, err := seatUp(t, "--attended", "--model", "gpt-6.1-sol", "--effort", "medium"); err != nil {
		t.Fatalf("mw seat up mayor --attended: %v\n%s", err, out)
	}
	line := strings.Join(theFakeCodexSaw(t, seen), " ")
	for _, want := range []string{"-a on-request", "model_reasoning_effort=medium"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q in %q", want, line)
		}
	}
}

func TestSeatUpOnACodexHostRefusesAClaudeModel(t *testing.T) {
	codexMemberHome(t)
	out, err := seatUp(t, "--model", "opus")
	if err == nil || !strings.Contains(err.Error(), "opus") {
		t.Fatalf("expected a refusal naming the model, got %q, %v", out, err)
	}
}
