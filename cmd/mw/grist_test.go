package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// runGrist runs mw grist with args under the test's home, reporting what it
// printed and how it ended.
func runGrist(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"grist"}, args...))
	err := root.Execute()
	return out.String(), err
}

// mw grist key makes the mill key where config says, once, and prints its
// public half and fingerprint, never its private key.
func TestGristKeyMakesTheMillKeyOnce(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	t.Setenv("MW_GRIST_KEY_FILE", "")
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "mw", "mill.key")

	first, err := runGrist(t, "key")
	if err != nil {
		t.Fatalf("mw grist key: %v\n%s", err, first)
	}
	wif, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the mill key at %s: %v", path, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the mill key 0600, got %v", info.Mode())
	}
	if !strings.Contains(first, "Wrote a new mill key to "+path) || strings.Contains(first, strings.TrimSpace(string(wif))) {
		t.Fatalf("expected the key made and its private half never printed, got:\n%s", first)
	}

	second, err := runGrist(t, "key")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(wif) || strings.Contains(second, "Wrote a new") {
		t.Fatalf("expected the key left as it was, got:\n%s", second)
	}
	var public string
	for _, line := range strings.Split(second, "\n") {
		if rest, ok := strings.CutPrefix(line, "public key: "); ok {
			public = rest
		}
	}
	if len(public) != 66 || !strings.Contains(second, "fingerprint: "+application.KeyFingerprint(public)) ||
		!strings.Contains(second, "POSTERN_MILL_KEY="+public) {
		t.Fatalf("expected the public key and its fingerprint, got:\n%s", second)
	}
}

// mw grist grind stops plainly when this machine has not been told its vault.
func TestGristGrindStopsWhenTheMachineDoesNotKnowItsVault(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	if out, err := runGrist(t, "grind"); err == nil || !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %v\n%s", err, out)
	}
}
