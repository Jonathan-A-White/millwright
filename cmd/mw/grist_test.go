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

// gristKeyAt runs mw grist key as host with the vault's home file saying
// home is home (no home file when it is empty), and the mill key where config
// says.
func gristKeyAt(t *testing.T, host, home string, args ...string) (string, error) {
	t.Helper()
	vault := t.TempDir()
	if home != "" {
		text := home + " 2026-09-29T00:10:00Z mw@" + home + "\n"
		if err := os.WriteFile(filepath.Join(vault, "home"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MW_VAULT", vault)
	t.Setenv("MW_HOST", host)
	return runGrist(t, append([]string{"key"}, args...)...)
}

func millKeyPath(t *testing.T) string {
	t.Helper()
	t.Setenv("MW_GRIST_KEY_FILE", "")
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mw", "mill.key")
}

// mw grist key is a lookup: with no key there it says so and how to make one,
// and creates nothing (mw-gq6.646).
func TestGristKeyWithNoKeyCreatesNothing(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	path := millKeyPath(t)

	out, err := gristKeyAt(t, "laptop", "laptop")
	if err == nil || !strings.Contains(err.Error()+out, "no mill key") || !strings.Contains(err.Error()+out, "--create") {
		t.Fatalf("expected it to say there is no mill key and name --create, got %v\n%s", err, out)
	}
	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("mw grist key made %s", path)
	}
}

// --create makes the mill key where config says, once, on the home host, and
// prints its public half and fingerprint, never its private key.
func TestGristKeyCreateMakesTheMillKeyOnce(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	path := millKeyPath(t)

	first, err := gristKeyAt(t, "laptop", "laptop", "--create")
	if err != nil {
		t.Fatalf("mw grist key --create: %v\n%s", err, first)
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

	// With the key there, plain mw grist key prints it as it always did, and
	// --create leaves it as it was.
	for _, args := range [][]string{nil, {"--create"}} {
		second, err := gristKeyAt(t, "laptop", "laptop", args...)
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
}

// --create is refused, and writes nothing, on a host that is not home, and on
// one whose home cannot be told, unless --force-host is given.
func TestGristKeyCreateIsRefusedOffHomeUnlessForced(t *testing.T) {
	for name, tc := range map[string]struct{ host, home string }{
		"a host that is not home":  {"cloud1", "laptop"},
		"a home that is not known": {"cloud1", ""},
	} {
		t.Run(name, func(t *testing.T) {
			mwConfig(t, "host = \"cloud1\"\n")
			path := millKeyPath(t)

			out, err := gristKeyAt(t, tc.host, tc.home, "--create")
			if err == nil || !strings.Contains(err.Error(), "--force-host") {
				t.Fatalf("expected a refusal naming --force-host, got %v\n%s", err, out)
			}
			if _, err := os.Lstat(path); err == nil {
				t.Fatalf("mw grist key --create made %s off home", path)
			}

			if out, err := gristKeyAt(t, tc.host, tc.home, "--create", "--force-host"); err != nil {
				t.Fatalf("mw grist key --create --force-host: %v\n%s", err, out)
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("expected --force-host to make %s: %v", path, err)
			}
		})
	}
}

// mw grist grind needs no vault: the mill reads no tracker, so a machine that
// has not been told its vault goes on to its key (mw-gq6.303).
func TestGristGrindNeedsNoVault(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	if out, err := runGrist(t, "grind"); err == nil || strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the mill to ask for no vault, got %v\n%s", err, out)
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

			out, err := gristKeyAt(t, "laptop", "laptop", "--create")
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

// mw grist eval <app> <kind> --example sends examples of the app's rig: its help
// names --example and the two-turn layout (turn_2), and it refuses an app and a
// kind with no example named, and the photo eval's flags beside them.
func TestGristEvalHelpNamesTheExampleModeAndTheTwoTurnLayout(t *testing.T) {
	help, err := runGrist(t, "eval", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--example", "<app> <kind>", "turn_2", "eval:<example>:<nonce>", "no history"} {
		if !strings.Contains(help, want) {
			t.Errorf("expected the help to name %q, got:\n%s", want, help)
		}
	}
	if _, err := runGrist(t, "eval", "lampas", "bible-talk"); err == nil || !strings.Contains(err.Error(), "--example") {
		t.Errorf("expected an app and a kind with no example to be refused, got %v", err)
	}
	if _, err := runGrist(t, "eval", "lampas", "bible-talk", "--example", "x", "--photos", "/tmp"); err == nil || !strings.Contains(err.Error(), "photo eval") {
		t.Errorf("expected the photo eval's flags to be refused beside an example, got %v", err)
	}
	if _, err := runGrist(t, "eval", "--example", "x"); err == nil {
		t.Error("expected an example with no app and kind to be refused")
	}
}
