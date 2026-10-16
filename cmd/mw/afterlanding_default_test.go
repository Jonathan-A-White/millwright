package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A Boost's config names the millwright checkout and no [after_landing] table,
// and its mw stayed as old as its last build (mw-gq6.521): the command is
// make build unless the host says otherwise, and runs as any landing's does.
func TestAHostWithTheMillwrightCheckoutAndNoAfterLandingEntryRunsMakeBuild(t *testing.T) {
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "Makefile"), []byte("build:\n\t@echo built > built.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "host = \"cloud1\"\n\n[rigs]\nmillwright = \"" + checkout + "\"\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	rigs, err := config.Rigs()
	if err != nil {
		t.Fatal(err)
	}
	commands, err := config.AfterLanding()
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	err = application.AfterLandingRun{
		AfterLanding: rig.NewAfterLanding(rig.WithAfterCommands(commands)),
		Rigs:         rigs,
		Host:         "cloud1",
		Out:          out,
	}.Run(t.Context(), "millwright")
	if err != nil {
		t.Fatalf("running the after-landing command: %v", err)
	}
	if want := "after landing: make build: ok"; !strings.Contains(out.String(), want) {
		t.Errorf("expected %q, got %q", want, out.String())
	}
	if _, err := os.Stat(filepath.Join(checkout, "built.txt")); err != nil {
		t.Errorf("expected make build to have run in the checkout: %v", err)
	}

	// An explicit empty entry runs nothing.
	cfg += "\n[after_landing]\nmillwright = \"\"\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if commands, err = config.AfterLanding(); err != nil {
		t.Fatal(err)
	}
	port := rig.NewAfterLanding(rig.WithAfterCommands(commands))
	if command := port.Command("millwright"); command != "" {
		t.Errorf("expected an explicit empty entry to name no command, got %q", command)
	}
}
