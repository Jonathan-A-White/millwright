package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	mwconfig "github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// bootstrapTableLine is contrib/boost-bootstrap.sh's own table_line, cut out
// of the script as it stands, so the test reads the exported file the way a
// new Boost does.
func bootstrapTableLine(t *testing.T) string {
	t.Helper()
	return bootstrapFunction(t, "table_line")
}

// bootstrapFunction is the named function of contrib/boost-bootstrap.sh, cut out
// of the script as it stands.
func bootstrapFunction(t *testing.T, name string) string {
	t.Helper()
	script, err := os.ReadFile(filepath.Join("..", "..", "contrib", "boost-bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	start := strings.Index(text, "\n"+name+"() {")
	if start < 0 {
		t.Fatalf("contrib/boost-bootstrap.sh has no %s function", name)
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("contrib/boost-bootstrap.sh's %s has no end", name)
	}
	return text[start+1 : start+end+3]
}

// TestConfigExportBoostWritesWhatBoostBootstrapReads runs the whole command on
// the home, against a real git vault, and reads every line back with the
// bootstrap's table_line: each rig's command comes back as the home wrote it,
// and a table the export does not carry gives nothing (mw-gq6.456).
func TestConfigExportBoostWritesWhatBoostBootstrapReads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash to run the bootstrap's table_line with")
	}
	home, vault := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", vault)
	t.Setenv("MW_HOST", "laptop")
	if out, err := exec.Command("git", "init", "-q", vault).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(vault, application.HomeFileName), []byte(laptopHome), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"millwright": "make test && scripts/check-codemap.sh",
		"argus":      `npm ci --no-audit --no-fund && npm test -- --grep "a\b"`,
		"postern":    "npm ci && TZ=UTC npm test && (cd server && go test ./...)",
	}
	specs := map[string]string{"lampas": "npm run build && TZ=UTC npx playwright test --project=shots {specs}"}
	config := `vault = "` + vault + `"
host = "laptop"

[tests]
millwright = "make test && scripts/check-codemap.sh"
argus = 'npm ci --no-audit --no-fund && npm test -- --grep "a\b"'
postern = "npm ci && TZ=UTC npm test && (cd server && go test ./...)"

[changed_specs]
lampas = "npm run build && TZ=UTC npx playwright test --project=shots {specs}"

[test_parts.lampas]
"install" = "npm ci --no-audit --no-fund"
"vitest 2/2" = "TZ=UTC npx vitest run --shard 2/2"
"vitest 1/2" = "TZ=UTC npx vitest run --shard 1/2"

[test_parts.postern]
"shots" = "npm run gate:shots"

[after_landing]
millwright = "make build"
`
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs([]string{"config", "export-boost"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw config export-boost: %v\n%s%s", err, stdout, stderr)
	}

	file := filepath.Join(vault, application.BoostConfigFile)
	tableLine := bootstrapTableLine(t)
	read := func(table, rig string) string {
		t.Helper()
		cmd := exec.Command("bash", "-c", tableLine+"\ntable_line \"$1\" \"$2\"", "bash", table, rig)
		cmd.Env = append(os.Environ(), "TABLES_FILE="+file)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("table_line %s %s: %v", table, rig, err)
		}
		return strings.TrimSuffix(string(out), "\n")
	}
	for table, want := range map[string]map[string]string{"tests": tests, "changed_specs": specs} {
		for rig, command := range want {
			line := read(table, rig)
			parsed, err := domain.ParseConfigTOML("[" + table + "]\n" + line + "\n")
			if err != nil {
				t.Fatalf("[%s] %s: the bootstrap reads %q, which a config.toml cannot hold: %v", table, rig, line, err)
			}
			literal, _ := parsed.Lookup(table, rig)
			if got, _ := domain.TOMLString(literal); got != command {
				t.Errorf("[%s] %s: the bootstrap reads %q, want the command %q", table, rig, line, command)
			}
		}
	}
	if line := read("after_landing", "millwright"); line != "" {
		t.Errorf("the export carries [after_landing]: %q", line)
	}

	// The [test_parts.<rig>] tables of the rigs a Boost clones come out of the
	// bootstrap's config_parts whole and in order, and a config.toml made of
	// them is what config.TestParts reads (mw-gq6.592).
	boost := t.TempDir()
	script := bootstrapFunction(t, "rig_list") + "\n" + bootstrapFunction(t, "parts_table") + "\n" + bootstrapFunction(t, "config_parts") + "\nconfig_parts"
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "TABLES_FILE="+file, "RIG_NAMES=lampas postern argus", "RIG=/rig")
	parts, err := cmd.Output()
	if err != nil {
		t.Fatalf("config_parts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(boost, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(boost, ".config", "mw", "config.toml"), append([]byte("host = \"cloud1\"\n"), parts...), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", boost)
	got, err := mwconfig.TestParts()
	if err != nil {
		t.Fatalf("config.TestParts on the copied config: %v", err)
	}
	want := map[string][]mwconfig.TestPart{
		"lampas": {
			{Name: "install", Command: "npm ci --no-audit --no-fund"},
			{Name: "vitest 2/2", Command: "TZ=UTC npx vitest run --shard 2/2"},
			{Name: "vitest 1/2", Command: "TZ=UTC npx vitest run --shard 1/2"},
		},
		"postern": {{Name: "shots", Command: "npm run gate:shots"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config.TestParts on the copied config = %+v, want %+v", got, want)
	}

	shown, err := exec.Command("git", "-C", vault, "show", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	if got := strings.TrimSpace(string(shown)); got != application.BoostConfigFile {
		t.Errorf("the vault's last commit holds %q, want %s alone", got, application.BoostConfigFile)
	}
}
