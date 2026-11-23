package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// paceHost makes a host whose own config says hostConfig and a vault whose
// pace.toml says vaultPace (either empty: no file), and returns the vault's dir.
func paceHost(t *testing.T, hostConfig, vaultPace string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(VaultEnv, "")
	vault := t.TempDir()
	if hostConfig != "" {
		path := filepath.Join(home, File)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(hostConfig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if vaultPace != "" {
		if err := os.WriteFile(filepath.Join(vault, PaceFile), []byte(vaultPace), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return vault
}

// The vault's pace.toml is the one source every host reads: a host that says
// nothing keeps the vault's count and line (mw-gq6.1698).
func TestPaceKeysComeFromTheVaultWhenTheHostSaysNothing(t *testing.T) {
	vault := paceHost(t, "", "[pace]\nmax_at_once = 5\nusage_line = 70\n")
	n, err := PaceMaxAtOnceIn(vault)
	if err != nil || n != 5 {
		t.Fatalf("max_at_once: got %d, %v; want 5", n, err)
	}
	line, err := PaceUsageLineIn(vault)
	if err != nil || line != 70 {
		t.Fatalf("usage_line: got %v, %v; want 70", line, err)
	}
}

// A host's own [pace] key is ignored when the vault's file says that key.
func TestPaceKeysOfTheVaultAnswerAheadOfTheHostsOwn(t *testing.T) {
	vault := paceHost(t, "[pace]\nmax_at_once = 3\nusage_line = 50\n", "[pace]\nmax_at_once = 5\n")
	if n, err := PaceMaxAtOnceIn(vault); err != nil || n != 5 {
		t.Fatalf("max_at_once: got %d, %v; want the vault's 5", n, err)
	}
	// The vault says nothing of usage_line, so the host's own still answers.
	if line, err := PaceUsageLineIn(vault); err != nil || line != 50 {
		t.Fatalf("usage_line: got %v, %v; want the host's 50", line, err)
	}
}

// With no vault file the host's own table and the defaults answer as before.
func TestPaceKeysFallBackToTheHostThenTheDefault(t *testing.T) {
	vault := paceHost(t, "[pace]\nmax_at_once = 4\n", "")
	if n, _ := PaceMaxAtOnceIn(vault); n != 4 {
		t.Errorf("max_at_once: got %d, want the host's 4", n)
	}
	if line, _ := PaceUsageLineIn(vault); line != DefaultPaceUsageLine {
		t.Errorf("usage_line: got %v, want the default", line)
	}
	if n, _ := PaceMaxAtOnceIn(""); n != 4 {
		t.Errorf("with no vault dir: got %d, want the host's 4", n)
	}
}

func TestPaceKeyOfTheVaultThatDoesNotReadIsAnError(t *testing.T) {
	vault := paceHost(t, "", "[pace]\nmax_at_once = lots\n")
	_, err := PaceMaxAtOnceIn(vault)
	if err == nil || !strings.Contains(err.Error(), PaceFile) {
		t.Fatalf("got %v, want an error naming %s", err, PaceFile)
	}
}

func TestPaceDifferencesNamesAHostKeyThatDisagreesWithTheVault(t *testing.T) {
	vault := paceHost(t, "[pace]\nmax_at_once = 3\nusage_line = 60.0\n", "[pace]\nmax_at_once = 5\nusage_line = 60\n")
	got, err := PaceDifferences(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "max_at_once") || !strings.Contains(got[0], "3") || !strings.Contains(got[0], "5") {
		t.Fatalf("got %q, want only max_at_once, saying 3 and 5", got)
	}
}

func TestPaceDifferencesIsEmptyWhenTheHostAgreesOrSaysNothing(t *testing.T) {
	vault := paceHost(t, "[pace]\nmax_at_once = 5\n", "[pace]\nmax_at_once = 5\nusage_line = 70\n")
	if got, err := PaceDifferences(vault); err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v; want none", got, err)
	}
	vault = paceHost(t, "[pace]\nmax_at_once = 7\n", "")
	if got, err := PaceDifferences(vault); err != nil || len(got) != 0 {
		t.Fatalf("with no vault file: got %q, %v; want none", got, err)
	}
}
