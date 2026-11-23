package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeStepConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if body == "" {
		return
	}
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.1676: a Builder's launch is given the guard unless the host's config
// says closeout.open_steps_guard = false.
func TestStepGuardSessionIsMwStepGuardUnlessTheConfigTurnsItOff(t *testing.T) {
	for _, body := range []string{"", "[closeout]\nopen_steps_guard = true\n"} {
		writeStepConfig(t, body)
		got := stepGuardSession()
		if len(got) != 3 || !slices.Equal(got[1:], []string{"step", "guard"}) {
			t.Errorf("config %q: expected `<mw> step guard`, got %v", body, got)
		}
	}
	writeStepConfig(t, "[closeout]\nopen_steps_guard = false\n")
	if got := stepGuardSession(); got != nil {
		t.Errorf("expected no guard when the config says false, got %v", got)
	}
}

func TestStepGovernorTakesAReason(t *testing.T) {
	cmd := newStepGovernorCmd()
	cmd.SetArgs([]string{"mw-gq6.1", "mw-mol-x"})
	cmd.SetOut(os.Stderr)
	cmd.SetErr(os.Stderr)
	if err := cmd.Execute(); err == nil {
		t.Errorf("mw step governor ran with no --why")
	}
}
