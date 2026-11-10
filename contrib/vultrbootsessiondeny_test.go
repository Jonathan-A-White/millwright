package contrib_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const denyLiteral = `["Bash(mw config set *)", "Bash(scp *)", "Read(~/.ssh/**)", "Bash(a \"b\" \\ c)"]`

// boostSessionDenyRig is boostAddressRig with a config.toml on the box (the box
// is this host: the ssh stand-in runs the script here); it returns the
// environment and the config's path.
func boostSessionDenyRig(t *testing.T, config string) (env []string, conf string) {
	t.Helper()
	env, _ = boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	conf = filepath.Join(home, ".config", "mw", "config.toml")
	if config != "" {
		if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(conf, []byte(config), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return env, conf
}

func TestVultrBoostSetSessionDenyChangesOnlyTheDenyLine(t *testing.T) {
	line := "deny = " + denyLiteral + "\n"
	for name, c := range map[string]struct{ before, after, said string }{
		"no session table": {
			"cap = 2\n\n[rigs]\nargus = \"/x\"\n",
			"cap = 2\n\n[rigs]\nargus = \"/x\"\n\n[session]\n" + line, "set"},
		"a different deny": {
			"cap = 2\n\n[session]\ndeny = [\"Bash(scp *)\"]\nother = 1\n\n[rigs]\nargus = \"/x\"\n",
			"cap = 2\n\n[session]\n" + line + "other = 1\n\n[rigs]\nargus = \"/x\"\n", "set"},
		"a deny over several lines": {
			"[session]\ndeny = [\n  \"Bash(scp *)\", # no\n  \"Read(~/.ssh/**)\",\n]\nother = 1\n[rigs]\nargus = \"/x\"\n",
			"[session]\n" + line + "other = 1\n[rigs]\nargus = \"/x\"\n", "set"},
		"a session table with no deny": {
			"[session]\nother = 1\n\n[rigs]\nargus = \"/x\"\n",
			"[session]\nother = 1\n" + line + "\n[rigs]\nargus = \"/x\"\n", "set"},
		"the same deny": {
			"cap = 2\n\n[session]\n" + line + "\n[rigs]\nargus = \"/x\"\n",
			"cap = 2\n\n[session]\n" + line + "\n[rigs]\nargus = \"/x\"\n", "level"},
	} {
		t.Run(name, func(t *testing.T) {
			env, conf := boostSessionDenyRig(t, c.before)
			out, errOut, err := runBoost(t, env, "set-session-deny", "--name", "cloud1", "--deny", denyLiteral)
			if err != nil {
				t.Fatalf("set-session-deny: %v: %s", err, errOut)
			}
			if !strings.HasSuffix(out, "session.deny = "+c.said+"\n") {
				t.Errorf("expected the last line to say %s, got %q", c.said, out)
			}
			if got := mustRead(t, conf); got != c.after {
				t.Errorf("config after:\n%s\nwant:\n%s", got, c.after)
			}
			if info, err := os.Stat(conf); err != nil || info.Mode().Perm() != 0o640 {
				t.Errorf("expected the config's mode kept at 640, got %v %v", info, err)
			}
			// Run again: now level, nothing changes.
			out, _, err = runBoost(t, env, "set-session-deny", "--name", "cloud1", "--deny", denyLiteral)
			if err != nil || !strings.HasSuffix(out, "session.deny = level\n") {
				t.Errorf("expected a second run to say level, got %q, %v", out, err)
			}
		})
	}
}

func TestVultrBoostSetSessionDenyNeedsABoxWithAConfigAndAnArray(t *testing.T) {
	env, _ := boostSessionDenyRig(t, "")
	if _, errOut, err := runBoost(t, env, "set-session-deny", "--name", "cloud1", "--deny", denyLiteral); err == nil {
		t.Errorf("expected a box with no config to be an error, got none (%s)", errOut)
	}
	env, conf := boostSessionDenyRig(t, "cap = 2\n")
	if _, errOut, err := runBoost(t, env, "set-session-deny", "--name", "cloud1", "--deny", "Bash(scp *)"); err == nil || !strings.Contains(errOut, "one-line TOML array") {
		t.Errorf("expected a deny that is not an array to be refused, got %v %q", err, errOut)
	}
	if got := mustRead(t, conf); got != "cap = 2\n" {
		t.Errorf("expected the config untouched, got %q", got)
	}
}

func TestVultrBoostSetSessionDenyDryRunRunsNothing(t *testing.T) {
	env, conf := boostSessionDenyRig(t, "cap = 2\n")
	out, errOut, err := runBoost(t, env, "set-session-deny", "--dry-run", "--name", "cloud1", "--deny", denyLiteral)
	if err != nil {
		t.Fatalf("dry run: %v: %s", err, errOut)
	}
	if !strings.Contains(out, "nothing below is run") {
		t.Errorf("expected a dry run to say so, got %q", out)
	}
	if got := mustRead(t, conf); got != "cap = 2\n" {
		t.Errorf("expected the config untouched, got %q", got)
	}
}
