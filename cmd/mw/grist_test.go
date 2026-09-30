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

// mw grist key refuses, and writes nothing, when grist_key_file is the
// Mayor's postern key file: the mill must have a key of its own, and the
// Mayor's key must never be the one that opens the phone's grist.
func TestGristKeyRefusesTheMayorsKeyFile(t *testing.T) {
	for name, tc := range map[string]struct {
		grist string
		link  bool
	}{
		"the same path":             {"$HOME/.config/mw/postern.key", false},
		"the same path, unclean":    {"$HOME/.config/mw/../mw/./postern.key", false},
		"a link to the Mayor's key": {"$HOME/mill-link.key", true},
	} {
		t.Run(name, func(t *testing.T) {
			mwConfig(t, "host = \"laptop\"\n")
			t.Setenv("MW_POSTERN_KEY_FILE", "")
			home, _ := os.UserHomeDir()
			mayors := filepath.Join(home, ".config", "mw", "postern.key")
			if tc.link {
				if err := os.Symlink(mayors, filepath.Join(home, "mill-link.key")); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("MW_GRIST_KEY_FILE", strings.ReplaceAll(tc.grist, "$HOME", home))

			out, err := runGrist(t, "key")
			if err == nil || !strings.Contains(err.Error(), "Mayor's postern key") {
				t.Fatalf("expected a refusal saying it is the Mayor's key, got %v\n%s", err, out)
			}
			if _, err := os.Lstat(mayors); err == nil {
				t.Errorf("mw grist key wrote %s", mayors)
			}
		})
	}
}

// mw grist eval documents every flag in its help and refuses to start
// without the two it needs.
func TestGristEvalHelpNamesEveryFlagAndTheTwoRequired(t *testing.T) {
	help, err := runGrist(t, "eval", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--grind", "--photos", "--models", "--effort", "--out"} {
		if !strings.Contains(help, flag) {
			t.Errorf("expected the help to document %s, got:\n%s", flag, help)
		}
	}
	if _, err := runGrist(t, "eval", "--photos", t.TempDir()); err == nil || !strings.Contains(err.Error(), "grind") {
		t.Fatalf("expected --grind required, got %v", err)
	}
}
