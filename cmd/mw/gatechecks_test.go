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
		checks, err := landingChecks(t.TempDir(), which)
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

// A rig whose file names timing_tests has its gate commands run with
// MW_TIMING=skip on every path that lands a story (mw-eo6dj9.2); a rig whose
// file has no such key is run with no MW_TIMING at all.
func TestLandingChecksRunsARigWithTimingTestsWithTheSkipEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[tests]\nlampas = \"env | grep '^MW_TIMING' | sort; true\"\nfellowship = \"env | grep '^MW_TIMING' | sort; true\"\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultDir, "rigs"), 0o755); err != nil {
		t.Fatal(err)
	}
	rigFile := "timing_tests = [\"tests/timing/**\"]\n"
	if err := os.WriteFile(filepath.Join(vaultDir, "rigs", "lampas.toml"), []byte(rigFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "rigs", "fellowship.toml"), []byte("union_merge_files = [\"docs/*.md\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, which := range map[string]gateCommands{
		"mw next and mw land": {related: true},
		"mw check":            {parts: true},
	} {
		checks, err := landingChecks(vaultDir, which)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		with, err := checks.Run(context.Background(), "lampas", t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := "MW_TIMING=skip\nMW_TIMING_TESTS=tests/timing/**\n"; with.Output != want {
			t.Errorf("%s: a rig with timing_tests ran with %q, want %q", name, with.Output, want)
		}
		without, err := checks.Run(context.Background(), "fellowship", t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if without.Output != "" {
			t.Errorf("%s: a rig without timing_tests ran with %q, want no MW_TIMING", name, without.Output)
		}
	}
}
