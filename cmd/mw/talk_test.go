package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// talkModelCmd runs mw talk model against a backend that takes a message, with
// a tmux first on PATH that logs every call it is given, so that a test can see
// that nothing reached any window. It returns what the command printed, the
// vault it logs in, and what tmux was asked.
func talkModelCmd(t *testing.T, args ...string) (out, vault string, tmuxCalls string, err error) {
	t.Helper()
	f := loadPosternRecordFixture(t)
	url, _ := fakePosternBackend(t, map[string]string{
		"/api/messages": `{"txid":"f00dfeed","seq":1}`,
	})
	posternHome(t, url, f.SenderWIF, f.RecipientPubKey)

	bin := t.TempDir()
	calls := filepath.Join(bin, "tmux-calls")
	script := "#!/bin/sh\necho \"$@\" >>" + calls + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for tmux: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	vault = configuredVault(t)

	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"talk", "model"}, args...))
	err = root.Execute()
	raw, _ := os.ReadFile(calls)
	return buf.String(), vault, string(raw), err
}

// configuredVault is the vault posternHome's config.toml names.
func configuredVault(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "mw", "config.toml"))
	if err != nil {
		t.Fatalf("reading the config: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "vault = "); ok {
			return strings.Trim(rest, `"`)
		}
	}
	t.Fatalf("the config names no vault:\n%s", raw)
	return ""
}

func TestTalkModelHelpRuns(t *testing.T) {
	out, _, _, err := talkModelCmd(t, "--help")
	if err != nil {
		t.Fatalf("mw talk model --help: %v", err)
	}
	for _, want := range []string{"opus|sonnet|fable|haiku", "--talk", "--turn", "bin/respawn-mayor"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the help to say %q, got:\n%s", want, out)
		}
	}
}

func TestTalkModelSpeaksPrintsTheRespawnLineAndTouchesNoWindow(t *testing.T) {
	out, vault, tmuxCalls, err := talkModelCmd(t, "sonnet", "--talk", "talk-7", "--turn", "3")
	if err != nil {
		t.Fatalf("mw talk model sonnet: %v\n%s", err, out)
	}
	if want := "hand off, then: bin/respawn-mayor high claude-sonnet-5-5\n"; out != want {
		t.Errorf("expected exactly %q printed, got %q", want, out)
	}
	if tmuxCalls != "" {
		t.Errorf("expected tmux never to be run, it was run with:\n%s", tmuxCalls)
	}
	log, err := os.ReadFile(filepath.Join(vault, ".mayor-talk.log"))
	if err != nil || !strings.Contains(string(log), "talk model sonnet: spoke the switch on talk talk-7 turn 3; hand off, then: bin/respawn-mayor high claude-sonnet-5-5") {
		t.Errorf("expected the talk log to say what was done, got %q (%v)", log, err)
	}
}

func TestTalkModelRefusesAnUnknownChipAndDoesNothing(t *testing.T) {
	out, vault, tmuxCalls, err := talkModelCmd(t, "gpt", "--talk", "talk-7", "--turn", "3")
	if err == nil || !strings.Contains(err.Error(), "opus, sonnet, fable or haiku") {
		t.Fatalf("expected a refusal naming the models, got %q, %v", out, err)
	}
	if tmuxCalls != "" {
		t.Errorf("expected a refused chip to run no tmux, it was run with:\n%s", tmuxCalls)
	}
	if _, statErr := os.Stat(filepath.Join(vault, ".mayor-talk.log")); !os.IsNotExist(statErr) {
		t.Errorf("expected a refused chip to log nothing, got %v", statErr)
	}
}
