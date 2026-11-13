package main

import (
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
