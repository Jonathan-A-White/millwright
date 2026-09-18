package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// writeConfig puts a config file under a fresh home directory and makes it the
// home directory of this test.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the config directory: %v", err)
	}
	if contents != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(contents), 0o644); err != nil {
			t.Fatalf("writing the config file: %v", err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	return home
}

func TestVaultPrefersTheEnvironment(t *testing.T) {
	writeConfig(t, "vault = \"/from/the/file\"\n")
	t.Setenv("MW_VAULT", "/from/the/environment")

	got, err := config.Vault()
	if err != nil {
		t.Fatalf("finding the vault: %v", err)
	}
	if got != "/from/the/environment" {
		t.Fatalf("expected MW_VAULT to win, got %q", got)
	}
}

func TestVaultFallsBackToTheConfigFile(t *testing.T) {
	writeConfig(t, "# where the beads live\nvault = \"/root/millwright-vault\"  # the one database\n")

	got, err := config.Vault()
	if err != nil {
		t.Fatalf("finding the vault: %v", err)
	}
	if got != "/root/millwright-vault" {
		t.Fatalf("expected the vault from the config file, got %q", got)
	}
}

func TestVaultReadsSingleQuotesAndBareValues(t *testing.T) {
	for _, line := range []string{"vault = '/root/millwright-vault'", "vault=/root/millwright-vault"} {
		writeConfig(t, line+"\n")
		got, err := config.Vault()
		if err != nil {
			t.Fatalf("finding the vault from %q: %v", line, err)
		}
		if got != "/root/millwright-vault" {
			t.Fatalf("expected the vault from %q, got %q", line, got)
		}
	}
}

func TestVaultIgnoresKeysInsideATable(t *testing.T) {
	writeConfig(t, "[laptop]\nvault = \"/elsewhere\"\n")

	if _, err := config.Vault(); err == nil {
		t.Fatal("expected a vault set inside a table to be ignored")
	}
}

func TestVaultSaysHowToSetItWhenItIsNotSet(t *testing.T) {
	writeConfig(t, "")

	_, err := config.Vault()
	if err == nil {
		t.Fatal("expected an error when the vault is set nowhere")
	}
	for _, want := range []string{"MW_VAULT", "config.toml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got %q", want, err.Error())
		}
	}
}
