package claude

// The harness's version is read by running a stand-in program in Claude Code's
// place, never the real one.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// standIn writes an executable script with the given body and returns its path.
func standIn(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVersionIsWhatTheProgramPrintsForDashDashVersion(t *testing.T) {
	program := standIn(t, `[ "$1" = "--version" ] && echo "9.8.7 (Claude Code)"`)

	got, err := New(WithProgram(program)).Version(context.Background())
	if err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	if got != "9.8.7 (Claude Code)" {
		t.Fatalf("got %q, want the program's own line without its newline", got)
	}
}

func TestVersionOfAProgramThatIsNotInstalledSaysSo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-claude")

	_, err := New(WithProgram(missing)).Version(context.Background())
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("got %v, want ErrNotInstalled", err)
	}
}

func TestVersionOfAProgramThatFailsIsAnErrorButNotNotInstalled(t *testing.T) {
	program := standIn(t, `echo "boom" >&2; exit 3`)

	_, err := New(WithProgram(program)).Version(context.Background())
	if err == nil || errors.Is(err, ErrNotInstalled) {
		t.Fatalf("got %v, want a plain failure", err)
	}
}

func TestVersionOfAProgramThatPrintsNothingIsAnError(t *testing.T) {
	program := standIn(t, `exit 0`)

	if _, err := New(WithProgram(program)).Version(context.Background()); err == nil {
		t.Fatal("expected an error for a program that printed no version")
	}
}
