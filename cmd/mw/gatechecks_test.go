package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
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

// mw land reads the gate's commands when the gate runs, not when the batch began
// (mw-gq6.1006): a line written to the config after landingChecks was built is the
// one run, and the Checked says when it was read and whether it differs.
func writeGateConfig(t *testing.T, home, config string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
}

func aRepoWithASpecAndASource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write("tests/e2e/a.spec.ts")
	write("src/a.ts")
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "change"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestMwLandRunsTheTestsLineWrittenAfterTheBatchBegan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeGateConfig(t, home, "[tests]\nlampas = \"echo old\"\n")
	checks, err := landingChecks(t.TempDir(), gateCommands{related: true, parts: true, reread: true})
	if err != nil {
		t.Fatal(err)
	}

	unchanged, err := checks.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Command != "echo old" || unchanged.Read.What != "tests" || unchanged.Read.At.IsZero() || unchanged.Read.Changed {
		t.Errorf("expected the line the batch began with, read now and not changed, got %+v", unchanged)
	}

	writeGateConfig(t, home, "[tests]\nlampas = \"echo new\"\n")
	changed, err := checks.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if changed.Command != "echo new" || strings.TrimSpace(changed.Output) != "new" || !changed.Read.Changed || changed.Read.At.IsZero() {
		t.Errorf("expected the new line run and said changed, got %+v", changed)
	}
}

func TestMwLandRunsTheRelatedLineWrittenAfterTheBatchBegan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeGateConfig(t, home, "[tests]\nlampas = \"true\"\n[related_tests]\nlampas = \"echo old {files}\"\n")
	dir := aRepoWithASpecAndASource(t)
	checks, err := landingChecks(t.TempDir(), gateCommands{related: true, reread: true})
	if err != nil {
		t.Fatal(err)
	}
	writeGateConfig(t, home, "[tests]\nlampas = \"true\"\n[related_tests]\nlampas = \"echo new {files}\"\n")

	run, err := checks.RunRelated(context.Background(), "lampas", dir, application.RelatedSpan{Base: "HEAD~1"})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Ran || !strings.HasPrefix(run.Command, "echo new ") || run.Read.What != "related_tests" || !run.Read.Changed || run.Read.At.IsZero() {
		t.Errorf("expected the new related-tests line run and said changed, got %+v", run)
	}
}

func TestMwLandRunsTheChangedSpecsLineWrittenAfterTheBatchBegan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeGateConfig(t, home, "[tests]\nlampas = \"true\"\n[changed_specs]\nlampas = \"echo old {specs}\"\n")
	dir := aRepoWithASpecAndASource(t)
	checks, err := landingChecks(t.TempDir(), gateCommands{reread: true})
	if err != nil {
		t.Fatal(err)
	}
	writeGateConfig(t, home, "[tests]\nlampas = \"true\"\n[changed_specs]\nlampas = \"echo new {specs}\"\n")

	run, err := checks.RunSpecs(context.Background(), "lampas", dir, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(run.Command, "echo new ") || run.Read.What != "changed_specs" || !run.Read.Changed || run.Read.At.IsZero() {
		t.Errorf("expected the new changed-specs line run and said changed, got %+v", run)
	}
}

func TestMwLandReadsTheGatePartsWhenTheyAreAsked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeGateConfig(t, home, "[tests]\nlampas = \"true\"\n[test_parts.lampas]\nunit = \"echo old\"\n")
	checks, err := landingChecks(t.TempDir(), gateCommands{parts: true, reread: true})
	if err != nil {
		t.Fatal(err)
	}
	writeGateConfig(t, home, "[tests]\nlampas = \"true\"\n[test_parts.lampas]\nunit = \"echo new\"\n")

	parts := checks.Parts("lampas")
	if len(parts) != 1 || parts[0].Command != "echo new" {
		t.Errorf("expected the part written after the batch began, got %+v", parts)
	}
}

func TestMwNextAndMwCheckKeepTheLinesTheyBeganWith(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeGateConfig(t, home, "[tests]\nlampas = \"echo old\"\n")
	checks, err := landingChecks(t.TempDir(), gateCommands{related: true})
	if err != nil {
		t.Fatal(err)
	}
	writeGateConfig(t, home, "[tests]\nlampas = \"echo new\"\n")
	run, err := checks.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if run.Command != "echo old" || !run.Read.At.IsZero() {
		t.Errorf("expected the line the process began with and nothing said of a reading, got %+v", run)
	}
}

func TestAGateCommandWhoseConfigCannotBeReadAtTheGateRunsTheLineTheBatchBeganWith(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeGateConfig(t, home, "[tests]\nlampas = \"echo old\"\n")
	checks, err := landingChecks(t.TempDir(), gateCommands{related: true, parts: true, reread: true})
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the file was: it opens, and cannot be read.
	file := filepath.Join(home, ".config", "mw", "config.toml")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o755); err != nil {
		t.Fatal(err)
	}

	run, err := checks.Run(context.Background(), "lampas", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if run.Command != "echo old" || strings.TrimSpace(run.Output) != "old" {
		t.Errorf("expected the line the batch began with run, got %+v", run)
	}
	if run.Read.Failed == "" || run.Read.Changed || !run.Read.At.IsZero() {
		t.Errorf("expected the failed reading said and no change, got %+v", run.Read)
	}
	if lines := run.Read.Lines(run.Command); len(lines) != 1 || !strings.Contains(lines[0], "could not be read again") {
		t.Errorf("expected the log to say why, got %q", lines)
	}
}
