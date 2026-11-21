package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestVoiceKeepAliveEnvMissingFileIsNoEnvFile(t *testing.T) {
	_, err := readVoiceKeepAliveEnv(filepath.Join(t.TempDir(), "church-voice.env"))
	if !errors.Is(err, application.ErrNoVoiceEnvFile) {
		t.Fatalf("expected ErrNoVoiceEnvFile, got %v", err)
	}
}

func TestVoiceKeepAliveEnvRefusesAFileOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "church-voice.env")
	if err := os.WriteFile(path, []byte("CHURCH_VOICE_APP_PASSWORD=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readVoiceKeepAliveEnv(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("expected a refusal naming chmod 600, got %v", err)
	}
}

func TestVoiceKeepAliveDryRunCommandPrintsTheMailAndNeverTheSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "church-voice.env")
	body := "CHURCH_VOICE_APP_PASSWORD=sekrit-app-pass\nCHURCH_VOICE_TEXT_ADDRESS=1.2@txt.voice.google.com\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newVoiceKeepAliveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--dry-run", "--env", path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"To: 1.2@txt.voice.google.com", "From: whiteplainsnac@gmail.com", "Testing"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "sekrit-app-pass") {
		t.Errorf("the dry run printed the password:\n%s", out.String())
	}
}

func TestVoiceKeepAliveDryRunWithNoFileSaysTheJobIsOff(t *testing.T) {
	cmd := newVoiceKeepAliveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--dry-run", "--env", filepath.Join(t.TempDir(), "none.env")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "off: no church-voice.env") || !strings.Contains(out.String(), "would post nothing") {
		t.Errorf("got:\n%s", out.String())
	}
}

func TestVoiceKeepAliveHelpSaysTheFileTurnsItOn(t *testing.T) {
	if !strings.Contains(newVoiceKeepAliveCmd().Long, "turns the job on") {
		t.Errorf("the help does not say the file turns the job on:\n%s", newVoiceKeepAliveCmd().Long)
	}
}
