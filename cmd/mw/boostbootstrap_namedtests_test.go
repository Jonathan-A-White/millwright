package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const namedTestsVault = `# header
[tests]
postern = "npm test"

[named_tests]
lampas = "npx vitest run -t {tests}"
postern = "npm test -- --grep {tests}"
other = "never cloned"

[test_parts.postern]
"shots" = "npm run gate:shots"
`

// bootstrapRun runs script after the named functions of contrib/boost-bootstrap.sh
// as they stand, with the vault file and the rigs a Boost has in its environment.
func bootstrapRun(t *testing.T, functions []string, script string, env ...string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash to run the bootstrap's functions with")
	}
	var text strings.Builder
	for _, name := range functions {
		text.WriteString(bootstrapFunction(t, name) + "\n")
	}
	text.WriteString(script)
	cmd := exec.Command("bash", "-c", text.String())
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	return string(out)
}

// TestBootstrapGivesABoostTheNamedTestsLines: a new Boost's config.toml holds the
// vault file's [named_tests] line of each rig it has, and an existing one that
// has no such table gets it appended, every other line kept, as a
// [test_parts.<rig>] table is (mw-gq6.592, mw-gq6.1223).
func TestBootstrapGivesABoostTheNamedTestsLines(t *testing.T) {
	dir := t.TempDir()
	vault := filepath.Join(dir, "boost-config.toml")
	if err := os.WriteFile(vault, []byte(namedTestsVault), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{"TABLES_FILE=" + vault, "RIG_NAMES=lampas postern", "RIG=/rig"}

	fresh := bootstrapRun(t, []string{"rig_list", "table_line", "config_table"}, "config_table named_tests", env...)
	want := "\n[named_tests]\nlampas = \"npx vitest run -t {tests}\"\npostern = \"npm test -- --grep {tests}\"\n"
	if fresh != want {
		t.Errorf("a new Boost's [named_tests] = %q, want %q", fresh, want)
	}

	// config_lines is what a new config is written from.
	lines := bootstrapRun(t, []string{"rig_list", "table_line", "config_table", "parts_table", "config_parts", "session_table", "config_session", "config_lines"},
		"config_lines", append(env, "BOOST_VAULT=/v", "BOOST_HOST=cloud1", "BOOST_CAP=2", "BOOST_BEADS_HOST=x", "BOOST_RIGS_DIR=/r", "MW_AFTER_LANDING=make build")...)
	if !strings.Contains(lines, want) {
		t.Errorf("config_lines lacks the [named_tests] table:\n%s", lines)
	}

	existing := filepath.Join(dir, "config.toml")
	old := "host = \"cloud1\"  # mine\n\n[rigs]\npostern = \"/r/postern\"\n\n[tests]\npostern = \"npm test\"\n"
	if err := os.WriteFile(existing, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	functions := []string{"rig_list", "table_line", "config_table", "has_table", "with_named_tests"}
	got := bootstrapRun(t, functions, "with_named_tests "+existing, env...)
	if !strings.HasPrefix(got, old) {
		t.Errorf("a line of the existing config was changed:\n%s", got)
	}
	if !strings.HasSuffix(got, want) {
		t.Errorf("the [named_tests] table is not appended at the end:\n%s", got)
	}

	// A config that has the table is left as it is.
	if err := os.WriteFile(existing, []byte(old+"\n[named_tests]\npostern = \"mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := bootstrapRun(t, functions, "with_named_tests "+existing, env...); got != old+"\n[named_tests]\npostern = \"mine\"\n" {
		t.Errorf("a config with a [named_tests] table was edited:\n%s", got)
	}
}

// TestBootstrapInstallsFfmpegForTheRigsThatConvertAudio: a fresh Boost's package
// list holds ffmpeg, the tool a rig's run-book test needs before it reaches what
// it checks. A cloud box without it failed the test on its own, and mw next took
// that for main's red (mw-gq6.1395).
func TestBootstrapInstallsFfmpegForTheRigsThatConvertAudio(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "contrib", "boost-bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var packages []string
	for _, line := range strings.Split(string(script), "\n") {
		if rest, ok := strings.CutPrefix(line, "PKGS="); ok {
			packages = strings.Fields(strings.Trim(rest, `"`))
		}
	}
	if len(packages) == 0 {
		t.Fatal("contrib/boost-bootstrap.sh has no PKGS line")
	}
	for _, want := range []string{"ffmpeg", "git", "tmux", "jq", "ripgrep", "wireguard-tools"} {
		found := false
		for _, p := range packages {
			found = found || p == want
		}
		if !found {
			t.Errorf("the packages a Boost installs lack %s: %v", want, packages)
		}
	}
}

// bootstrapPackagesLine runs contrib/boost-bootstrap.sh --dry-run on a box where
// no package is installed, with a vault that holds the given rigs/ files (none
// when rigs is nil), and returns the apt-get install line of its packages step.
func bootstrapPackagesLine(t *testing.T, rigs map[string]string) (line string, pkgs []string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash to run the bootstrap with")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "contrib", "boost-bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(text), "\n") {
		if rest, ok := strings.CutPrefix(l, "PKGS="); ok {
			pkgs = strings.Fields(strings.Trim(rest, `"`))
		}
	}
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if rigs != nil {
		if err := os.MkdirAll(filepath.Join(vault, "rigs"), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range rigs {
			if err := os.WriteFile(filepath.Join(vault, "rigs", name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	bin := filepath.Join(dir, "bin")
	home := filepath.Join(dir, "home")
	for _, d := range []string{bin, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a box with nothing installed: dpkg-query finds no package
	if err := os.WriteFile(filepath.Join(bin, "dpkg-query"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a sudo that notes what it was asked and does nothing: a read of a root-only
	// file on this host must never reach the real sudo (mw-gq6.1594).
	sudoCalls := filepath.Join(dir, "sudo-calls")
	stub := "#!/bin/sh\necho \"$*\" >>" + sudoCalls + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root")
	cmd, err := bootstrapScriptCmd(script, []string{
		"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "BOOST_VAULT=" + vault,
		"BOOST_HOST=cloud9", "BOOST_WG_ADDRESS=10.88.0.9", "BOOST_BEADS_HOST=10.88.0.1",
		"BOOST_WG_HUB_PUBKEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"BOOST_ROOT=" + root,
	}, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// the run stops at the vault step (the temp vault is no checkout); the
	// packages step is printed first.
	out, _ := cmd.CombinedOutput()
	if calls, err := os.ReadFile(sudoCalls); err == nil {
		for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
			for _, arg := range strings.Fields(call) {
				if strings.HasPrefix(arg, "/") && !strings.HasPrefix(arg, "//") && !strings.HasPrefix(arg, root+"/") { // "//p" is sed's own
					t.Errorf("the dry run asked sudo to read a path of this host: %q (BOOST_ROOT is not the prefix of every read)", call)
				}
			}
		}
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "would: apt-get install") {
			line = strings.TrimSpace(l)
			break
		}
	}
	if line == "" {
		t.Fatalf("the dry run printed no apt-get install line:\n%s", out)
	}
	return line, pkgs
}

func countWord(line, word string) int {
	n := 0
	for _, f := range strings.Fields(line) {
		if f == word {
			n++
		}
	}
	return n
}

// TestBootstrapInstallsTheSystemPackagesOfTheRigsInTheVault: a rig's file in the
// vault may list system_packages, and the packages step installs the union of
// those and mw's own, each once (mw-gq6.1396).
func TestBootstrapInstallsTheSystemPackagesOfTheRigsInTheVault(t *testing.T) {
	line, pkgs := bootstrapPackagesLine(t, map[string]string{
		"lampas.toml":  "files_expected = true\nsystem_packages = [\"ffmpeg\", \"sox\"]\nsite_url = \"https://x/\"\n",
		"cairn.toml":   "files_expected = true\n",
		"voicey.toml":  "system_packages = [\n  \"espeak\", # speech\n  \"fontconfig\",\n]\n",
		"strange.toml": "# system_packages = [\"nope\"]\nnote = \"system_packages = [\\\"nope2\\\"]\"\n",
	})
	if got := countWord(line, "ffmpeg"); got != 1 {
		t.Errorf("ffmpeg appears %d times, want once: %s", got, line)
	}
	for _, want := range append(append([]string{}, pkgs...), "sox", "espeak", "fontconfig") {
		if countWord(line, want) != 1 {
			t.Errorf("%s is not named once: %s", want, line)
		}
	}
	for _, not := range []string{"nope", "nope2"} {
		if countWord(line, not) != 0 {
			t.Errorf("%s is named though no rig lists it: %s", not, line)
		}
	}
}

// TestBootstrapPackagesAreMwsOwnWhenNoRigListsAny: a rig file with no
// system_packages key, or a vault with no rigs/ folder, changes nothing.
func TestBootstrapPackagesAreMwsOwnWhenNoRigListsAny(t *testing.T) {
	for name, rigs := range map[string]map[string]string{
		"a rig file without the key": {"cairn.toml": "files_expected = true\n"},
		"no rigs folder":             nil,
	} {
		line, pkgs := bootstrapPackagesLine(t, rigs)
		want := "would: apt-get install -y --no-install-recommends " + strings.Join(pkgs, " ")
		if line != want {
			t.Errorf("%s: line = %q, want %q", name, line, want)
		}
	}
}

// bootstrapScriptCmd is the one way a test runs contrib/boost-bootstrap.sh whole:
// the script reads /etc/wireguard/wg0.conf and more with `sudo -n` under
// BOOST_ROOT, and with it unset that is this host's own file, so env must name
// BOOST_ROOT as a folder under the temp dir (mw-gq6.1594).
func bootstrapScriptCmd(script string, env []string, args ...string) (*exec.Cmd, error) {
	root := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "BOOST_ROOT="); ok {
			root = v
		}
	}
	if root == "" {
		return nil, errBootRootUnset
	}
	if !strings.HasPrefix(filepath.Clean(root), filepath.Clean(os.TempDir())+string(filepath.Separator)) {
		return nil, errBootRootNotTemp
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = env
	return cmd, nil
}

var (
	errBootRootUnset   = errors.New("BOOST_ROOT is unset: the script would read this host's real /etc and /root through sudo -n")
	errBootRootNotTemp = errors.New("BOOST_ROOT is not under the temp dir: the script would read a real path of this host")
)

// TestBootstrapScriptCmdRefusesARunThatCouldReadTheHostsRoot: a run of the whole
// script without BOOST_ROOT, or with one outside the temp dir, is refused.
func TestBootstrapScriptCmdRefusesARunThatCouldReadTheHostsRoot(t *testing.T) {
	temp := filepath.Join(t.TempDir(), "root")
	for name, tc := range map[string]struct {
		env  []string
		want error
	}{
		"unset":     {[]string{"HOME=/h"}, errBootRootUnset},
		"empty":     {[]string{"BOOST_ROOT="}, errBootRootUnset},
		"the host":  {[]string{"BOOST_ROOT=/"}, errBootRootNotTemp},
		"elsewhere": {[]string{"BOOST_ROOT=/etc"}, errBootRootNotTemp},
		"a temp":    {[]string{"BOOST_ROOT=" + temp}, nil},
	} {
		if _, err := bootstrapScriptCmd("s.sh", tc.env); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

// TestEveryTestThatRunsTheWholeBootstrapScriptGoesThroughBootstrapScriptCmd reads
// the test sources of this package: none builds its own bash command on the
// script, so none can leave BOOST_ROOT unset (mw-gq6.1594).
func TestEveryTestThatRunsTheWholeBootstrapScriptGoesThroughBootstrapScriptCmd(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	needle := "exec.Command(\"bash\", " + "script"
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, needle) && !strings.Contains(line, "append([]string{script}") {
				t.Errorf("%s runs the bootstrap script itself: %s; use bootstrapScriptCmd", f, strings.TrimSpace(line))
			}
		}
	}
}
