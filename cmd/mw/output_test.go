package main

import (
	"os"
	"path/filepath"
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
// list, so a read-only command can be run without a beads database, and points
// mw at an empty vault as the host `vps`.
func standInBeads(t *testing.T) {
	t.Helper()

	bin := t.TempDir()
	script := "#!/bin/sh\necho '[]'\n"
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
