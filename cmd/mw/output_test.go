package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A report is what mw was asked for, so it goes to stdout, where a pipe or a
// redirect finds it; stderr is left for errors. cobra sends cmd.Print* to
// stderr unless the command's output was set, and the real binary never sets
// it, so these tests do not either: they swap the process's own stdout and
// stderr for files and read which one the report landed in.

// runSeparately runs mw with args and returns what it wrote to stdout and to
// stderr, apart.
func runSeparately(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()

	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer outFile.Close()
	errFile, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer errFile.Close()

	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	root := newRootCmd()
	root.SetArgs(args)
	runErr := root.Execute()
	os.Stdout, os.Stderr = realOut, realErr

	if runErr != nil {
		t.Fatalf("mw %s failed: %v", strings.Join(args, " "), runErr)
	}
	return readAll(t, outFile.Name()), readAll(t, errFile.Name())
}

func readAll(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

// standInBeads puts a `bd` on PATH that answers every question with an empty
// list — and `kv list`, which status now reads for the Mayor's needs, with an
// empty map — so a read-only command can be run without a beads database, and points
// mw at an empty vault as the host `vps`.
func standInBeads(t *testing.T) {
	t.Helper()

	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *\"kv list\"*) echo '{}'; exit 0;; esac\necho '[]'\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MW_VAULT", t.TempDir())
	t.Setenv("MW_HOST", "vps")
}

func TestVersionReportsOnStdoutAndNothingOnStderr(t *testing.T) {
	stdout, stderr := runSeparately(t, "version")

	if !strings.Contains(stdout, version) {
		t.Fatalf("expected the version %q on stdout, got %q", version, stdout)
	}
	if stderr != "" {
		t.Fatalf("expected nothing on stderr, got %q", stderr)
	}
}

func TestStatusReportsOnStdoutAndNothingOnStderr(t *testing.T) {
	standInBeads(t)

	stdout, stderr := runSeparately(t, "status")

	if !strings.Contains(stdout, "mw status") || !strings.Contains(stdout, "RUNNING") {
		t.Fatalf("expected the status report on stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected nothing on stderr, got %q", stderr)
	}
}

// writeBeadsBudget gives the host a config.toml whose beads_budget_bytes is
// budget, and a vault whose .beads holds size bytes.
func writeBeadsBudget(t *testing.T, budget, size int64) {
	t.Helper()

	home := os.Getenv("HOME")
	conf := filepath.Join(home, ".config", "mw", "config.toml")
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("beads_budget_bytes = "+strconv.FormatInt(budget, 10)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beads := filepath.Join(os.Getenv("MW_VAULT"), ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beads, "data"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatusWarnsPastTheBeadsBudgetTheConfigSays(t *testing.T) {
	standInBeads(t)
	t.Setenv("MW_BEADS_BUDGET_BYTES", "")
	writeBeadsBudget(t, 3_000_000, 2_000_000)

	stdout, _ := runSeparately(t, "status")
	if !strings.Contains(stdout, "BEADS 2MB") || strings.Contains(stdout, "past the") {
		t.Fatalf("expected 2MB under a 3MB budget to draw no warning, got %q", stdout)
	}

	writeBeadsBudget(t, 1_000_000, 2_000_000)
	stdout, _ = runSeparately(t, "status")
	if !strings.Contains(stdout, "BEADS 2MB: past the 1MB budget") {
		t.Fatalf("expected the warning to name the configured 1MB budget, got %q", stdout)
	}
}

func TestStatusRefusesABeadsBudgetNamingTheKey(t *testing.T) {
	standInBeads(t)
	t.Setenv("MW_BEADS_BUDGET_BYTES", "")
	writeBeadsBudget(t, 0, 10)

	root := newRootCmd()
	root.SetArgs([]string{"status"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "beads_budget_bytes") {
		t.Fatalf("expected status to refuse a zero budget naming beads_budget_bytes, got %v", err)
	}
}

func TestStatusSaysWhetherTheNetworkIsMetered(t *testing.T) {
	standInBeads(t)

	t.Setenv("MW_METERED", "yes")
	stdout, stderr := runSeparately(t, "status")
	if !strings.Contains(stdout, "NETWORK metered (config: metered = \"yes\")") || stderr != "" {
		t.Fatalf("expected a metered NETWORK line, got %q (stderr %q)", stdout, stderr)
	}

	t.Setenv("MW_METERED", "no")
	if stdout, _ = runSeparately(t, "status"); !strings.Contains(stdout, "NETWORK unmetered\n") {
		t.Fatalf("expected an unmetered NETWORK line, got %q", stdout)
	}
}
