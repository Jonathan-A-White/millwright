package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeEnvFile is ~/.config/mw/claude.env, under the host's home.
func TestClaudeEnvFileIsUnderTheHomeConfig(t *testing.T) {
	t.Setenv("HOME", "/h")
	if got := claudeEnvFile(); got != "/h/.config/mw/claude.env" {
		t.Errorf("expected /h/.config/mw/claude.env, got %q", got)
	}
}

// Every Claude session cmd/mw starts reads the one-year token: no command
// builds a grinder or a harness except through claudeGrinder and claudeHarness
// (mw-gq6.829), which pass the claude env file.
func TestNoCommandStartsClaudeOutsideTheTokenReadingHelpers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, "claude.NewGrinder(") && !(file == "sync.go" && strings.Contains(line, "WithGrindClaudeEnvFile(claudeEnvFile())")) {
				t.Errorf("%s:%d builds a grinder that does not read claude.env: use claudeGrinder()", file, n+1)
			}
			if strings.Contains(line, "claude.New(") && !(file == "sync.go" && strings.Contains(line, "claude.New(opts...)")) && !(file == "version.go") {
				t.Errorf("%s:%d builds a harness that does not read claude.env: use claudeHarness", file, n+1)
			}
		}
	}
}
