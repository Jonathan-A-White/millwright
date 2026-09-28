package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runPosternHandCmd runs one mw command line and reports its output and error.
func runPosternHandCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// posternHandConfig is a throwaway home whose config names the Governor's
// key and a backend, with every postern setting in the environment cleared.
func posternHandConfig(t *testing.T) string {
	t.Helper()
	for _, env := range []string{
		"MW_POSTERN_BACKEND", "MW_POSTERN_GOVERNOR_KEY", "MW_POSTERN_KEY_FILE",
		"MW_POSTERN_SNAPSHOT_PATH", "MW_POSTERN_VIEW_PATH",
	} {
		t.Setenv(env, "")
	}
	mwConfig(t, "vault = \"/v\"\nhost = \"desktop\"\npostern_backend = \"http://10.88.0.3:8787\"\npostern_governor_key = \"02governor\"\n")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestPosternServeDefaultsEveryValueFromThisHostsConfigAndKey(t *testing.T) {
	home := posternHandConfig(t)
	if out, err := runPosternHandCmd(t, "postern", "key", "init"); err != nil {
		t.Fatalf("mw postern key init: %v\n%s", err, out)
	}
	shown, err := runPosternHandCmd(t, "postern", "key", "show")
	if err != nil {
		t.Fatalf("mw postern key show: %v", err)
	}
	envFile := filepath.Join(home, "postern.env")

	out, err := runPosternHandCmd(t, "postern", "serve", "--dry-run", "--env-file", envFile, "--addr", "10.88.0.3:8787", "--mw", "/opt/mw")
	if err != nil {
		t.Fatalf("mw postern serve --dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{
		`postern_backend = "http://10.88.0.3:8787"`,
		`postern_governor_key = "02governor"`,
		`postern_view_path = "` + filepath.Join(home, ".local", "state", "postern", "view.b64") + `"`,
		`POSTERN_ADDR="10.88.0.3:8787"`,
		`POSTERN_BEAD_CMD="/opt/mw postern bead"`,
		`POSTERN_ON_MESSAGE="/opt/mw postern inbox --apply"`,
		`POSTERN_ISSUER_KEY="02governor"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the dry run to show %s, got:\n%s", want, out)
		}
	}
	mayor := ""
	for _, field := range strings.Fields(shown) {
		if len(field) == 66 && (strings.HasPrefix(field, "02") || strings.HasPrefix(field, "03")) {
			mayor = field
		}
	}
	if mayor == "" || !strings.Contains(out, `POSTERN_MAYOR_KEY="`+mayor+`"`) {
		t.Errorf("expected the Mayor's key %q from mw postern key show in the dry run, got:\n%s", mayor, out)
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("expected --dry-run to write no environment file, got %v", err)
	}
}

func TestPosternServeWithAnEnvironmentFileAndNoKeySaysHowToGetOne(t *testing.T) {
	home := posternHandConfig(t)

	_, err := runPosternHandCmd(t, "postern", "serve", "--dry-run", "--env-file", filepath.Join(home, "postern.env"))
	if err == nil || !strings.Contains(err.Error(), "mw postern key init") {
		t.Fatalf("expected the refusal to say how to make the key, got %v", err)
	}
}

func TestPosternNginxDefaultsItsBackendFromThisHostsConfig(t *testing.T) {
	home := posternHandConfig(t)
	conf := filepath.Join(home, "postern.conf")
	site := "server {\n    location /api/ {\n        proxy_pass http://laptop.mw:8787;\n    }\n    # mw-api end\n}\n"
	if err := os.WriteFile(conf, []byte(site), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPosternHandCmd(t, "postern", "nginx", "--dry-run", "--conf", conf)
	if err != nil {
		t.Fatalf("mw postern nginx --dry-run: %v\n%s", err, out)
	}
	if strings.Contains(out, "laptop.mw") || strings.Count(out, "proxy_pass http://10.88.0.3:8787;") != 2 {
		t.Fatalf("expected both /api locations pointed at postern_backend, got:\n%s", out)
	}
	if !strings.Contains(out, "location = /api/events {") {
		t.Fatalf("expected the events location, got:\n%s", out)
	}
}
