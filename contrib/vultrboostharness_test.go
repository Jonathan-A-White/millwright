package contrib_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boostHarnessRig is boostAddressRig with a claude and an npm on the box's
// PATH (the box is this host: the ssh stand-in runs the script here). claude
// prints the version kept in a file, as Claude Code does ("2.1.300 (Claude
// Code)"); npm notes its calls and, unless failWith is set, installs the
// version named after the @ by writing it into that file. It returns the
// environment, the file npm notes its calls in and the file the version is in.
func boostHarnessRig(t *testing.T, installed, failWith string) (env []string, npmCalls, versionFile string) {
	t.Helper()
	env, _ = boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\n", boostWg0)
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	npmCalls = filepath.Join(home, "npm-calls")
	versionFile = filepath.Join(home, "claude-version")
	bin := filepath.Join(home, "harness-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	npm := "#!/bin/bash\necho \"$*\" >>" + npmCalls + "\n"
	if failWith != "" {
		npm += "echo 'npm warn postinstall is not covered by allowScripts'\necho '" + failWith + "' >&2\nexit 1\n"
	} else {
		npm += "echo 'npm warn postinstall is not covered by allowScripts'\n" +
			"v=${@: -1}; printf '%s\\n' \"${v##*@}\" >" + versionFile + "\nexit 0\n"
	}
	files := map[string]string{
		filepath.Join(bin, "claude"): "#!/bin/bash\n[ \"$1\" = --version ] && echo \"$(cat " + versionFile + ") (Claude Code)\"\n",
		filepath.Join(bin, "npm"):    npm,
		versionFile:                  installed + "\n",
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			env[i] = "PATH=" + bin + ":" + v
		}
	}
	return env, npmCalls, versionFile
}

func TestVultrBoostSetHarnessInstallsTheHomesVersionOnABoxBehindIt(t *testing.T) {
	env, npmCalls, versionFile := boostHarnessRig(t, "2.1.300", "")
	out, errOut, err := runBoost(t, env, "set-harness", "--name", "cloud1", "--version", "2.1.322")
	if err != nil {
		t.Fatalf("set-harness: %v: %s", err, errOut)
	}
	if !strings.HasSuffix(out, "harness = 2.1.300 -> 2.1.322\n") {
		t.Errorf("expected the version before and after on the last line, got %q", out)
	}
	calls := strings.Split(strings.TrimSpace(mustRead(t, npmCalls)), "\n")
	home := filepath.Dir(npmCalls)
	want := "install -g --prefix " + home + "/.local @anthropic-ai/claude-code@2.1.322"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("expected exactly one install, %q, got %q", want, calls)
	}
	if got := strings.TrimSpace(mustRead(t, versionFile)); got != "2.1.322" {
		t.Errorf("expected the box's claude at 2.1.322, got %q", got)
	}
}

func TestVultrBoostSetHarnessInstallsNothingOnABoxAlreadyLevel(t *testing.T) {
	for _, installed := range []string{"2.1.322", "2.1.330", "2.2.0", "2.1.1000"} {
		env, npmCalls, _ := boostHarnessRig(t, installed, "")
		out, errOut, err := runBoost(t, env, "set-harness", "--name", "cloud1", "--version", "2.1.322")
		if err != nil {
			t.Fatalf("set-harness at %s: %v: %s", installed, err, errOut)
		}
		if !strings.HasSuffix(out, "harness = "+installed+" level\n") {
			t.Errorf("at %s: expected the box said level, got %q", installed, out)
		}
		if _, err := os.Stat(npmCalls); err == nil {
			t.Errorf("at %s: npm was called: %s", installed, mustRead(t, npmCalls))
		}
	}
}

func TestVultrBoostSetHarnessComparesVersionsAsNumbersNotText(t *testing.T) {
	env, npmCalls, _ := boostHarnessRig(t, "2.1.99", "")
	if _, errOut, err := runBoost(t, env, "set-harness", "--name", "cloud1", "--version", "2.1.322"); err != nil {
		t.Fatalf("set-harness: %v: %s", err, errOut)
	}
	if got := strings.TrimSpace(mustRead(t, npmCalls)); !strings.HasSuffix(got, "@2.1.322") {
		t.Errorf("2.1.99 is behind 2.1.322 and should be installed over, got npm calls %q", got)
	}
}

func TestVultrBoostSetHarnessFailedInstallLeavesTheBoxUpAndSaysWhy(t *testing.T) {
	env, npmCalls, versionFile := boostHarnessRig(t, "2.1.300", "npm error network timeout")
	out, errOut, err := runBoost(t, env, "set-harness", "--name", "cloud1", "--version", "2.1.322")
	if err != nil {
		t.Fatalf("a failed install must not fail the bring-up: %v: %s", err, errOut)
	}
	last := out[strings.LastIndex(strings.TrimSuffix(out, "\n"), "\n")+1:]
	if want := "harness = 2.1.300 stays: npm install failed: npm error network timeout\n"; last != want {
		t.Errorf("expected one line saying the harness stays and why, %q, got %q", want, last)
	}
	if n := strings.Count(mustRead(t, npmCalls), "\n"); n != 1 {
		t.Errorf("expected one install tried, got %d", n)
	}
	if got := strings.TrimSpace(mustRead(t, versionFile)); got != "2.1.300" {
		t.Errorf("expected the box left at 2.1.300, got %q", got)
	}
}

func TestVultrBoostSetHarnessInstallThatLeavesTheOldVersionSaysSo(t *testing.T) {
	env, _, versionFile := boostHarnessRig(t, "2.1.300", "")
	// npm succeeds but claude on the PATH is another copy that does not change.
	if err := os.WriteFile(filepath.Join(filepath.Dir(versionFile), "harness-bin", "npm"), []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runBoost(t, env, "set-harness", "--name", "cloud1", "--version", "2.1.322")
	if err != nil {
		t.Fatalf("set-harness: %v: %s", err, errOut)
	}
	if want := "harness = 2.1.300 stays: after the install claude --version says 2.1.300, not 2.1.322\n"; !strings.HasSuffix(out, want) {
		t.Errorf("expected %q at the end, got %q", want, out)
	}
}

func TestVultrBoostSetHarnessNeedsAVersionThatIsNumbers(t *testing.T) {
	env, npmCalls, _ := boostHarnessRig(t, "2.1.300", "")
	for _, version := range []string{"", "latest", "2.1.322; rm -rf ~"} {
		_, errOut, err := runBoost(t, env, "set-harness", "--name", "cloud1", "--version", version)
		if err == nil || !strings.Contains(errOut, "--version") {
			t.Errorf("version %q: expected a refusal naming --version, got %v: %q", version, err, errOut)
		}
	}
	if _, err := os.Stat(npmCalls); err == nil {
		t.Errorf("npm was called: %s", mustRead(t, npmCalls))
	}
}

func TestVultrBoostSetHarnessDryRunRunsNothing(t *testing.T) {
	env, npmCalls, _ := boostHarnessRig(t, "2.1.300", "")
	out, errOut, err := runBoost(t, env, "set-harness", "--dry-run", "--name", "cloud1", "--version", "2.1.322")
	if err != nil {
		t.Fatalf("set-harness --dry-run: %v: %s", err, errOut)
	}
	if !strings.Contains(out, "set-harness --dry-run: nothing below is run") || !strings.Contains(out, "@anthropic-ai/claude-code@2.1.322") {
		t.Errorf("expected the steps to be printed, got %q", out)
	}
	if _, err := os.Stat(npmCalls); err == nil {
		t.Errorf("--dry-run called npm: %s", mustRead(t, npmCalls))
	}
}
