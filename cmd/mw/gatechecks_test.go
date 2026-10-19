package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every path that lands a story gets the rig's changed-specs command from the
// one constructor (mw-gq6.627): mw next, mw land and mw check each build their
// Checks with it, so a rig's [changed_specs] line cannot be missed by one.
func TestLandingChecksCarriesTheRigsChangedSpecsCommandOnEveryPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[tests]\nlampas = \"true\"\n[changed_specs]\nlampas = \"echo ran {specs}\"\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests", "e2e"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tests", "e2e", "a.spec.ts"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "spec"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	for name, which := range map[string]gateCommands{
		"mw next and mw land": {related: true},
		"mw check":            {parts: true},
	} {
		checks, err := landingChecks(which)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		run, err := checks.RunSpecs(context.Background(), "lampas", dir, "HEAD~1")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !run.Passed || !strings.Contains(run.Command, "echo ran tests/e2e/a.spec.ts") {
			t.Fatalf("%s: expected the rig's changed-specs command run on the spec, got %+v", name, run)
		}
	}
}
