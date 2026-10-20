package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const creditsTestKey = "sk-ant-test-key-for-the-command"

// creditsHome makes a throwaway home directory with a config, a vault whose
// home is homeHost, and this host named host; nothing reaches the real ~/.config/mw.
func creditsHome(t *testing.T, host, homeHost string) (home string) {
	t.Helper()
	home = t.TempDir()
	vault := filepath.Join(home, "vault")
	for _, dir := range []string{filepath.Join(home, ".config", "mw"), vault} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	record := homeHost + " 2026-09-29T00:10:00Z mw@" + homeHost + "\n"
	if err := os.WriteFile(filepath.Join(vault, application.HomeFileName), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	config := "vault = \"" + vault + "\"\nhost = \"" + host + "\"\n[grist]\ndaily_limit = 5\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_HOST", "")
	t.Setenv("MW_GRIST_STATE_DIR", "")
	return home
}

func runCredits(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"grist", "credits"}, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestCreditsKeyWritesTheFileAtHomeAndShowsNoKey(t *testing.T) {
	home := creditsHome(t, "laptop", "laptop")
	stdout, stderr, err := runCredits(t, creditsTestKey+"\n", "key")
	if err != nil {
		t.Fatalf("mw grist credits key: %v", err)
	}
	path := filepath.Join(home, ".config", "mw", "anthropic-api.key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %04o", info.Mode().Perm())
	}
	if raw, _ := os.ReadFile(path); strings.TrimSpace(string(raw)) != creditsTestKey {
		t.Errorf("the key file holds %q", raw)
	}
	for name, said := range map[string]string{"stdout": stdout, "stderr": stderr} {
		if strings.Contains(said, creditsTestKey) {
			t.Errorf("the key is in %s: %s", name, said)
		}
	}
	// Neither an event log, the config nor the credits state holds the key:
	// the key file is the only file under the home that does.
	_ = filepath.WalkDir(home, func(file string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || file == path {
			return nil
		}
		if raw, _ := os.ReadFile(file); strings.Contains(string(raw), creditsTestKey) {
			t.Errorf("the key is in %s", file)
		}
		return nil
	})
}

func TestCreditsKeyIsRefusedAwayFromHomeAndForABadLine(t *testing.T) {
	for name, tc := range map[string]struct{ host, line, want string }{
		"not home":    {"cloud1", creditsTestKey + "\n", "is not home"},
		"empty line":  {"laptop", "\n", "the line was empty"},
		"wrong start": {"laptop", "sk-wrong-" + creditsTestKey + "\n", "does not start with sk-ant-"},
	} {
		t.Run(name, func(t *testing.T) {
			home := creditsHome(t, tc.host, "laptop")
			stdout, stderr, err := runCredits(t, tc.line, "key")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected a refusal saying %q, got %v", tc.want, err)
			}
			if _, statErr := os.Stat(filepath.Join(home, ".config", "mw", "anthropic-api.key")); statErr == nil {
				t.Error("a key file was written")
			}
			for _, said := range []string{stdout, stderr, err.Error()} {
				if strings.Contains(said, strings.TrimSpace(tc.line)) && strings.TrimSpace(tc.line) != "" {
					t.Errorf("the line was repeated: %s", said)
				}
			}
		})
	}
}

func TestCreditsOnOffAndStatusReadTheHomesConfig(t *testing.T) {
	home := creditsHome(t, "laptop", "laptop")
	if stdout, _, err := runCredits(t, "", "on"); err != nil || !strings.Contains(stdout, "api credits are now on") {
		t.Fatalf("on: %v\n%s", err, stdout)
	}
	if raw, _ := os.ReadFile(filepath.Join(home, ".config", "mw", "config.toml")); !strings.Contains(string(raw), "api_credits = true") {
		t.Errorf("the config does not say api_credits = true:\n%s", raw)
	}
	stdout, _, err := runCredits(t, "")
	if err != nil || !strings.HasPrefix(stdout, "api credits: on") || !strings.Contains(stdout, "key file: no, ") {
		t.Fatalf("status: %v\n%s", err, stdout)
	}
	if stdout, _, err := runCredits(t, "", "status", "--json"); err != nil || !strings.Contains(stdout, `"api_credits": true`) {
		t.Fatalf("status --json: %v\n%s", err, stdout)
	}
	if stdout, _, err := runCredits(t, "", "off"); err != nil || !strings.Contains(stdout, "api credits are now off") {
		t.Fatalf("off: %v\n%s", err, stdout)
	}
}

func TestReadKeyLineReadsOneLineFromAPipe(t *testing.T) {
	var prompt bytes.Buffer
	line, err := readKeyLine(strings.NewReader("sk-ant-x\nmore\n"), &prompt)
	if err != nil || strings.TrimSpace(line) != "sk-ant-x" || prompt.Len() != 0 {
		t.Errorf("got %q, %v, prompt %q", line, err, prompt.String())
	}
}
