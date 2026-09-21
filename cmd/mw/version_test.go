package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionCommandPrintsAVersion(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("mw version failed: %v", err)
	}

	printed := strings.TrimSpace(out.String())
	if printed == "" {
		t.Fatal("mw version printed nothing")
	}
	if !strings.Contains(printed, version) {
		t.Fatalf("expected %q to contain the version %q", printed, version)
	}
}

func buildInfo(settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Settings: settings}
}

func TestVersionLineRenderings(t *testing.T) {
	const revision = "e98c47d1f0a2b3c4d5e6f708192a3b4c5d6e7f80"

	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "a clean build prints the short revision",
			info: buildInfo(
				debug.BuildSetting{Key: "vcs.revision", Value: revision},
				debug.BuildSetting{Key: "vcs.modified", Value: "false"},
			),
			want: "mw 0.1.0-dev (e98c47d)",
		},
		{
			name: "a build from a dirty tree is marked modified",
			info: buildInfo(
				debug.BuildSetting{Key: "vcs.revision", Value: revision},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			want: "mw 0.1.0-dev (e98c47d, modified)",
		},
		{
			name: "build info without VCS settings prints the version alone",
			info: buildInfo(debug.BuildSetting{Key: "GOOS", Value: "linux"}),
			want: "mw 0.1.0-dev",
		},
		{
			name: "no build info prints the version alone",
			info: nil,
			want: "mw 0.1.0-dev",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := versionLine("0.1.0-dev", c.info); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// harnessOnPath puts a program named claude, with the given body, first on PATH.
func harnessOnPath(t *testing.T, body string) {
	t.Helper()

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestVersionPrintsTheHarnessVersionBesideMws(t *testing.T) {
	harnessOnPath(t, `echo "9.8.7 (Claude Code)"`)

	stdout, stderr := runSeparately(t, "version")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "mw "+version) {
		t.Fatalf("expected mw's line first, got %q", stdout)
	}
	if lines[1] != "claude 9.8.7 (Claude Code)" {
		t.Fatalf("expected the harness's own version on the second line, got %q", lines[1])
	}
	if stderr != "" {
		t.Fatalf("expected nothing on stderr, got %q", stderr)
	}
}

func TestVersionSaysPlainlyWhenTheHarnessIsNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	stdout, stderr := runSeparately(t, "version")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 || lines[1] != "claude: not installed" {
		t.Fatalf("expected mw's line and a plain 'not installed', got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected nothing on stderr, got %q", stderr)
	}
}

func TestVersionSaysPlainlyWhenTheHarnessWillNotTellItsVersion(t *testing.T) {
	harnessOnPath(t, `exit 3`)

	stdout, _ := runSeparately(t, "version")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "claude: version unknown") {
		t.Fatalf("expected mw's line and a plain 'version unknown', got %q", stdout)
	}
}

func TestRootCommandIsNamedMw(t *testing.T) {
	if got := newRootCmd().Name(); got != "mw" {
		t.Fatalf("expected the binary to be named mw, got %q", got)
	}
}
