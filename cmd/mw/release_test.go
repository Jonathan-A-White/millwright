package main

import (
	"bytes"
	"strings"
	"testing"
)

// mw release is never run against the factory's own vault here: it writes, and
// what it writes is the release of real work. What is checked is the wiring —
// that the command is there, that it takes exactly one epic, and that it stops
// before it reaches beads at all when this machine does not know where its
// vault is.

func TestReleaseCommandIsPartOfMw(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Name() == "release" {
			if err := cmd.Args(cmd, []string{}); err == nil {
				t.Error("expected mw release to need an epic id")
			}
			if err := cmd.Args(cmd, []string{"mw-1", "mw-2"}); err == nil {
				t.Error("expected mw release to take one epic id at a time")
			}
			return
		}
	}
	t.Fatal("expected mw to have a release command")
}

func TestReleaseStopsWhenTheMachineDoesNotKnowItsVault(t *testing.T) {
	t.Setenv("MW_VAULT", "")
	t.Setenv("HOME", t.TempDir())

	out, printed := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(printed)
	root.SetArgs([]string{"release", "mw-1"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw release to stop when it does not know where the vault is")
	}
	if !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %q", err)
	}
	if exitCode(err) != 1 {
		t.Errorf("expected mw to leave with 1, got %d", exitCode(err))
	}
	if out.Len() != 0 {
		t.Fatalf("expected nothing to be printed before the vault is known, got %q", out)
	}
}
